// Package clusterconfig loads the administrator-owned remote cluster catalogue.
// Loading checks local configuration and public certificates only: it never
// contacts a daemon, creates a transport or handles user credentials.
package clusterconfig

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v2"
)

const (
	maxConfigBytes = 256 << 10
	maxCABytes     = 1 << 20
	maxClusters    = 64
	maxNodes       = 200
)

// Cluster is a copy of one validated target. Public HTTPS trust is snapshotted
// at startup. JWT signature verification belongs exclusively to the daemon.
type Cluster struct {
	Ref               string
	Name              string
	ExpectedClusterID string
	Nodes             map[string]string
	CAFile            string
	CAPEM             []byte
	TLSInsecure       bool
	RequestTimeout    time.Duration
}

// Catalog is immutable after Load. Accessors return independent copies.
type Catalog struct{ clusters []Cluster }

func (c *Catalog) List() []Cluster {
	if c == nil {
		return nil
	}
	result := make([]Cluster, len(c.clusters))
	for i, cluster := range c.clusters {
		result[i] = clone(cluster)
	}
	return result
}

func (c *Catalog) Lookup(ref string) (Cluster, bool) {
	if c != nil {
		for _, cluster := range c.clusters {
			if cluster.Ref == ref {
				return clone(cluster), true
			}
		}
	}
	return Cluster{}, false
}

func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.clusters)
}

func (c *Catalog) LookupID(id string) (Cluster, bool) {
	if c != nil {
		for _, cluster := range c.clusters {
			if cluster.ExpectedClusterID == id {
				return clone(cluster), true
			}
		}
	}
	return Cluster{}, false
}

func clone(c Cluster) Cluster {
	c.Nodes = maps.Clone(c.Nodes)
	c.CAPEM = slices.Clone(c.CAPEM)
	return c
}

// text prevents YAML scalar coercions (for example a boolean used as a name).
type text string

func (t *text) UnmarshalYAML(unmarshal func(any) error) error {
	var value any
	if err := unmarshal(&value); err != nil {
		return err
	}
	s, ok := value.(string)
	if !ok {
		return fmt.Errorf("expected text")
	}
	*t = text(s)
	return nil
}

type document struct {
	Version  version             `yaml:"version"`
	Clusters map[text]definition `yaml:"clusters"`
}

type version int

func (v *version) UnmarshalYAML(unmarshal func(any) error) error {
	var value any
	if err := unmarshal(&value); err != nil {
		return err
	}
	n, ok := value.(int)
	if !ok {
		return fmt.Errorf("expected integer version")
	}
	*v = version(n)
	return nil
}

// boolean prevents strings or numbers from enabling a security-sensitive flag.
type boolean bool

func (b *boolean) UnmarshalYAML(unmarshal func(any) error) error {
	var value any
	if err := unmarshal(&value); err != nil {
		return err
	}
	v, ok := value.(bool)
	if !ok {
		return fmt.Errorf("expected boolean")
	}
	*b = boolean(v)
	return nil
}

type definition struct {
	Name              text          `yaml:"name"`
	ExpectedClusterID text          `yaml:"expected_cluster_id"`
	Nodes             map[text]text `yaml:"nodes"`
	TLS               struct {
		CAFile   text    `yaml:"ca_file"`
		Insecure boolean `yaml:"insecure"`
	} `yaml:"tls"`
	RequestTimeout text `yaml:"request_timeout"`
}

// Load rejects invalid or ambiguous configuration before a listener is opened.
// Parser errors are deliberately not echoed: they may contain file values.
func Load(path string) (*Catalog, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("cluster configuration file path must be absolute")
	}
	data, err := readBounded(path, maxConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("cluster configuration file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.SetStrict(true)
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("cluster configuration must be valid YAML with unique keys, known fields and matching value types")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("cluster configuration must contain exactly one YAML document")
	}
	if doc.Version != 2 {
		return nil, fmt.Errorf("version: only configuration version 2 is supported; configure issuer-to-endpoint nodes")
	}
	if len(doc.Clusters) == 0 || len(doc.Clusters) > maxClusters {
		return nil, fmt.Errorf("clusters: provide between 1 and %d clusters", maxClusters)
	}
	refs := make([]string, 0, len(doc.Clusters))
	for ref := range doc.Clusters {
		if !validRef(string(ref)) {
			return nil, fmt.Errorf("clusters: references must contain 1 to 64 ASCII letters, digits, hyphens or underscores")
		}
		refs = append(refs, string(ref))
	}
	slices.Sort(refs)
	catalog := &Catalog{}
	ids := make(map[string]bool)
	for _, ref := range refs {
		cluster, err := validate(ref, doc.Clusters[text(ref)])
		if err != nil {
			return nil, fmt.Errorf("clusters.%s.%w", ref, err)
		}
		if ids[cluster.ExpectedClusterID] {
			return nil, fmt.Errorf("clusters.%s.expected_cluster_id: duplicate cluster identity", ref)
		}
		ids[cluster.ExpectedClusterID] = true
		catalog.clusters = append(catalog.clusters, cluster)
	}
	return catalog, nil
}

func validate(ref string, d definition) (Cluster, error) {
	c := Cluster{Ref: ref, Name: string(d.Name), ExpectedClusterID: string(d.ExpectedClusterID), CAFile: string(d.TLS.CAFile), TLSInsecure: bool(d.TLS.Insecure), Nodes: make(map[string]string)}
	if !validText(c.Name, 128) {
		return Cluster{}, fmt.Errorf("name: provide 1 to 128 bytes of text without surrounding whitespace or control characters")
	}
	// OpenSVC identifiers are compared exactly; do not assume every configured
	// cluster uses a UUID, or substitute its display name for its identity.
	if !validText(c.ExpectedClusterID, 256) {
		return Cluster{}, fmt.Errorf("expected_cluster_id: provide a nonempty identifier of at most 256 bytes")
	}
	if len(d.Nodes) == 0 || len(d.Nodes) > maxNodes {
		return Cluster{}, fmt.Errorf("nodes: provide between 1 and %d issuer-to-HTTPS-origin mappings", maxNodes)
	}
	origins := make(map[string]bool)
	for node, endpoint := range d.Nodes {
		if !validText(string(node), 256) {
			return Cluster{}, fmt.Errorf("nodes: issuer names must be nonempty text of at most 256 bytes")
		}
		origin, err := endpointOrigin(string(endpoint))
		if err != nil {
			return Cluster{}, fmt.Errorf("nodes: provide HTTPS origins without credentials, path, query or fragment")
		}
		if origins[origin] {
			return Cluster{}, fmt.Errorf("nodes: duplicate endpoint")
		}
		origins[origin] = true
		c.Nodes[string(node)] = origin
	}
	timeout, err := time.ParseDuration(string(d.RequestTimeout))
	if err != nil || timeout < time.Second || timeout > 2*time.Minute {
		return Cluster{}, fmt.Errorf("request_timeout: provide a duration between 1s and 2m")
	}
	c.RequestTimeout = timeout
	if c.TLSInsecure && c.CAFile != "" {
		return Cluster{}, fmt.Errorf("tls: insecure=true and ca_file cannot be combined")
	}
	// An omitted TLS CA uses system roots; an explicit file replaces them.
	if c.CAFile == "" {
		return c, nil
	}
	if !filepath.IsAbs(c.CAFile) || strings.TrimSpace(c.CAFile) != c.CAFile {
		return Cluster{}, fmt.Errorf("tls.ca_file: provide an absolute file path")
	}
	c.CAPEM, err = readBounded(c.CAFile, maxCABytes)
	if err != nil {
		return Cluster{}, fmt.Errorf("tls.ca_file: %w", err)
	}
	if err := validateCA(c.CAPEM); err != nil {
		return Cluster{}, fmt.Errorf("tls.ca_file: %w", err)
	}
	return c, nil
}

func endpointOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || strings.TrimSpace(raw) != raw || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" && u.Path != "/" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("invalid origin")
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("invalid port")
		}
	}
	// Normalize equivalent origin spellings.
	host := strings.ToLower(u.Hostname())
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" || address.Is6() != strings.HasPrefix(u.Host, "[") {
			return "", fmt.Errorf("invalid IP origin")
		}
		host = address.String()
		if address.Is6() {
			host = "[" + host + "]"
		}
	} else if strings.HasPrefix(u.Host, "[") || !validHostname(host) {
		return "", fmt.Errorf("invalid hostname")
	}
	if port != 443 {
		host += ":" + strconv.Itoa(port)
	}
	return "https://" + host, nil
}

func validHostname(host string) bool {
	if len(host) > 253 {
		return false
	}
	// Numeric addresses must parse as IPs, rather than pass as DNS names.
	if strings.Trim(host, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func validRef(ref string) bool {
	if len(ref) == 0 || len(ref) > 64 {
		return false
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validText(s string, limit int) bool {
	return len(s) > 0 && len(s) <= limit && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

func readBounded(path string, limit int64) ([]byte, error) {
	// Check regular files before opening, so a FIFO cannot block startup.
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file is absent or unreadable")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file is absent or unreadable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("file must be readable and at most %d bytes", limit)
	}
	return data, nil
}

func validateCA(data []byte) error {
	count := 0
	for rest := bytes.TrimSpace(data); len(rest) > 0; rest = bytes.TrimSpace(rest) {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return fmt.Errorf("must contain only PEM CA certificates, without private keys or extra text")
		}
		// Decode only the first block: pem.Decode otherwise skips malformed
		// blocks and can silently accept a later valid certificate.
		endMarker := []byte("-----END CERTIFICATE-----")
		end := bytes.Index(rest, endMarker)
		if end < 0 {
			return fmt.Errorf("invalid PEM certificate")
		}
		end += len(endMarker)
		block, remaining := pem.Decode(rest[:end])
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(bytes.TrimSpace(remaining)) != 0 {
			return fmt.Errorf("invalid PEM certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.BasicConstraintsValid || !cert.IsCA || cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return fmt.Errorf("each certificate must be a valid CA certificate")
		}
		count++
		rest = rest[end:]
	}
	if count == 0 {
		return fmt.Errorf("must contain at least one PEM CA certificate")
	}
	return nil
}
