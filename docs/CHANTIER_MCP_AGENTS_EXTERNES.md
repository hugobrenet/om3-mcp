# Chantier V1 — Agent externe → MCP → multi-clusters OpenSVC

Document de cadrage issu des échanges de conception du 7 octobre 2026.
Mise à jour du 7 octobre 2026 : l'étape 2 est implémentée et le parcours Codex → authentik → découverte des outils MCP a été validé au lab. L'échange de tokens et les appels aux daemons restent à implémenter et à valider.

Ce fichier de travail est situé hors du dépôt Git `om3-mcp`. Une copie est versionnée dans `om3-mcp/docs/CHANTIER_MCP_AGENTS_EXTERNES.md` lors de la clôture de l'étape 2.

## Objectif

Permettre à un agent externe, par exemple Codex ou Claude Code, de consulter plusieurs clusters via un MCP central hébergé chez le client, sans lui communiquer les tokens acceptés par les daemons OpenSVC.

Le client externe reçoit un access token destiné au MCP. Le MCP valide ce token, résout la cible et échange ce token côté serveur contre un access token destiné au daemon du cluster demandé. Le SSO délivre les grants OpenSVC de l'utilisateur dans le token aval ; le daemon les applique. La V1 n'ajoute pas de politique métier d'autorisation dans le MCP.

Contexte cible : environ 800 clusters et 2 000 nœuds. La stack AI est en développement, sans contrainte de migration de production à ce stade.

## Périmètre et décisions retenues

- Se concentrer sur le MCP du daemon ; le MCP du Collector est hors périmètre.
- Mettre de côté l'intégration `om ai` / webapp pour ce chantier. Leur coexistence avec ce nouveau parcours fera l'objet d'une décision ultérieure.
- Utiliser OAuth pour l'accès du client externe au MCP et un échange de tokens côté serveur pour l'accès aux daemons.
- Concevoir une intégration indépendante d'authentik, avec authentik et Keycloak comme premières cibles de validation.
- Conserver provisoirement un catalogue administré dans `clusters.yaml`.
- Résoudre les clusters par leur nom pour la découverte, puis utiliser leur `cluster_id` pour l'exécution et la sélection de l'audience du daemon.
- Exiger le cluster dans les appels métier. Pour une opération sur un nœud, utiliser le couple `(cluster_id, node)`.
- Ne pas résoudre automatiquement un cluster à partir du seul nodename. Un même nodename peut exister dans plusieurs clusters.
- Ne pas imposer de liste des nœuds dans le catalogue de cette V1.
- Ne définir aucun scope métier MCP et aucun contrôle d'accès aux outils fondé sur de tels scopes en V1.
- Exposer tous les outils actuellement proposés par le MCP, avec les paramètres de cible nécessaires au parcours multi-clusters, et ajouter `list_clusters`. Leur exécution dépend des grants OpenSVC délivrés par le SSO et vérifiés par le daemon.
- Retourner tous les clusters configurés dans `list_clusters`, sous réserve de la recherche et de la pagination, à tout utilisateur authentifié au MCP. Il s'agit des clusters configurés, pas des clusters autorisés.
- Ne pas maintenir de règles locales utilisateur / client / cluster dans le MCP en V1. Conserver la validation des tokens, la résolution dans le catalogue administré et les refus du SSO et du daemon.
- Retenir `https://dev5-vip.opensvc.com:8443/mcp` comme URL du MCP et identifiant de ressource OAuth pour ce déploiement. Cette URL reste indépendante du cluster demandé.
- Garder les URLs, credentials et paramètres d'échange sous le contrôle du MCP, hors des arguments fournis par le LLM.
- Laisser les clarifications et la synthèse au LLM du client externe. Le MCP reste déterministe et fournit outils, résultats structurés et erreurs exploitables.

## Parcours attendu

```text
Opérateur : « Donne-moi le statut de dev5n1 dans dev5
             et de dev4n1 dans dev4. »

Client externe → serveur d'autorisation : connexion OAuth avec PKCE
Client externe ← serveur d'autorisation : access token destiné au MCP

Client externe → MCP : list_clusters(...)
MCP → client externe : noms et cluster_id de tous les clusters configurés

Client externe → MCP : get_node_status(UUID-dev5, dev5n1)
  MCP : validation du token destiné au MCP et présence de dev5 au catalogue
  MCP : résolution UUID-dev5 → VIP-dev5 + profil d'authentification
  MCP → serveur d'autorisation : échange authentifié du token MCP
  MCP ← serveur d'autorisation : access token destiné au daemon dev5
  MCP → VIP-dev5 : requête avec le token daemon
  Daemon : contrôle des grants OpenSVC et exécution ou refus
  MCP → client externe : résultat structuré

Client externe → MCP : get_node_status(UUID-dev4, dev4n1)
  Même parcours, avec les paramètres du cluster dev4

Client externe : synthèse des résultats pour l'opérateur
```

Un même token MCP peut servir aux deux appels. Chaque appel ne réussit que si le SSO accepte l'échange pour la cible et si les grants du token aval permettent l'opération au daemon. Les tokens daemon restent côté serveur. Les données retournées aux outils restent, elles, accessibles au client externe et potentiellement à son fournisseur de modèle.

L'access token aval est un JWT accepté par l'authentification OpenID du daemon ; ce n'est pas un ID token OpenID.

## Catalogue proposé

Schéma indicatif à implémenter, non accepté par le parseur actuel. Les valeurs de dev4 sont illustratives.

```yaml
version: 3

clusters:
  dev5:
    name: dev5
    cluster_id: 3bc5a684-0f37-4504-9f50-4107ff8d1f24
    endpoint: https://dev5-vip.opensvc.com:1215
    auth:
      profile: customer-sso
      audience: om3-dev5
    request_timeout: 20s

  dev4:
    name: dev4
    cluster_id: "<UUID réel de dev4>"
    endpoint: https://dev4-vip.opensvc.com:1215
    auth:
      profile: customer-sso
      audience: om3-dev4
    request_timeout: 20s
```

Le profil d'authentification est configuré séparément : issuer, métadonnées du serveur d'autorisation, client confidentiel du MCP, méthode d'authentification, référence au secret et paramètres d'échange nécessaires au SSO. Les paramètres de confiance TLS doivent rester configurables.

Le catalogue décrit les destinations. Sa visibilité ne donne pas, à elle seule, le droit d'exécuter une opération. Le SSO contrôle l'émission du token pour la cible ; le daemon contrôle les grants de ce token. Le MCP ne duplique pas ces règles dans une politique locale en V1.

## Contrat de découverte et comportement du LLM

Exemple de réponse de `list_clusters`, représentant les clusters configurés sans filtrage par autorisation utilisateur :

```json
{
  "items": [
    {
      "cluster_id": "3bc5a684-0f37-4504-9f50-4107ff8d1f24",
      "name": "dev5"
    }
  ],
  "next_cursor": null
}
```

- Prévoir une recherche par nom et une pagination bornée.
- Ne pas interroger chaque daemon ni tenter un échange de token par cluster pour construire cette liste. Elle est issue du catalogue et ne garantit ni l'accès ni la disponibilité d'une cible.
- Ne pas retourner les tokens, secrets, audiences ou endpoints internes pour cette découverte.
- Si le cluster est absent de la demande et du contexte explicite, le LLM demande de le préciser.
- Si plusieurs clusters portent le même nom, le LLM demande un élément discriminant avant l'appel métier.
- Si un outil exige un nœud et que celui-ci est absent, le LLM demande lequel cibler.
- Ne pas introduire de « cluster courant » mutable partagé entre appels.
- Les descriptions guident le LLM ; les schémas et contrôles MCP imposent les paramètres de cible. Le SSO et le daemon restent responsables des autorisations métier.

## OAuth et portabilité

Le socle visé est le profil d'autorisation MCP, Authorization Code avec PKCE et OAuth Token Exchange RFC 8693. La prise en charge d'OAuth/OIDC seule ne garantit pas celle de toute cette chaîne.

La V1 ne définit pas de scopes métier MCP : pas de `opensvc:status:read`, pas de scope par cluster et pas de filtrage des outils selon des scopes. Les métadonnées de ressource protégée publient la ressource `https://dev5-vip.opensvc.com:8443/mcp` et le serveur d'autorisation ; `scopes_supported` peut être omis en l'absence de scopes à annoncer. Les éventuels scopes techniques nécessaires à l'émission des claims par le SSO relèvent de la configuration de l'échange, pas d'une politique de permissions des outils dans le MCP.

Authentik et Keycloak ont des sémantiques différentes pour les audiences, les scopes et les claims émis. Il faut valider des configurations concrètes, et non supposer qu'un changement d'URL suffit. Pour Keycloak, privilégier l'échange standard dans un même realm pour la V1 ; les échanges entre domaines d'identité restent hors du premier parcours validé.

Le token aval doit porter les grants OpenSVC délivrés par le SSO pour cet utilisateur et cette cible, notamment via le claim `entitlements` attendu par le daemon. Le MCP ne les réécrit pas. Les mappings de l'échange doivent être vérifiés : les claims du token initial ne sont pas nécessairement recopiés. Les refus d'échange ou du daemon sont propagés, sans remplacement par un compte de service privilégié. Une restriction supplémentaire de la délégation propre aux agents externes est reportée à la V2.

## Todo list

### 1. Fixer les contrats de la V1

- [x] Choisir les outils exposés : tous les outils MCP actuels, plus `list_clusters`, avec adaptation des paramètres de cible.
- [x] Retenir l'absence de scopes métier et de politique locale d'autorisation dans le MCP pour la V1.
- [x] Confier au SSO l'émission des grants utilisateur et au daemon leur application.
- [x] Définir `list_clusters` comme la découverte de tous les clusters configurés, sans filtrage par droits utilisateur.
- [ ] Fixer les champs du catalogue, du profil d'authentification et de la configuration TLS.
- [x] Choisir l'URL du service MCP et son identifiant de ressource OAuth : `https://dev5-vip.opensvc.com:8443/mcp`.

Les cases cochées de cette section représentent des décisions de conception, pas des fonctionnalités implémentées.

### 2. Autoriser le client externe à accéder au MCP — implémenté, parcours Codex validé

- [x] Publier les métadonnées de ressource protégée et les challenges `WWW-Authenticate` appropriés, avec `resource_name`, sans `resource_documentation` ni `scopes_supported`.
- [x] Configurer le parcours Authorization Code avec PKCE et un client public préinscrit `om3-mcp` dans authentik de lab.
- [x] Valider avec Codex la découverte du serveur d'autorisation, le callback et le parcours utilisant le paramètre `resource` et un mapping explicite d'audience.
- [x] Vérifier les JWT entrants localement : issuer de confiance, audience MCP, signature asymétrique via JWKS et validité temporelle. Aucun scope métier MCP n'est requis.
- [ ] Définir la durée des tokens, la stratégie de renouvellement et le délai effectif de révocation.
- [x] Refuser les tokens invalides sans repli vers un autre mécanisme d'authentification.

#### Recette lab du 7 octobre 2026

- Branche : `feature/external-agent-oauth`.
- MCP / ressource OAuth : `https://dev5-vip.opensvc.com:8443/mcp`.
- Issuer : `https://labauthentik.opensvc.com/application/o/om3-mcp/`.
- Application et identifiant du client public : `om3-mcp`.
- Client testé : Codex CLI 0.160.1, login `codex mcp login opensvc --no-browser`, puis nouvelle session, également testée avec `codex --no-daemon`.
- Callback enregistré dans authentik en mode Strict / Authorization : `http://127.0.0.1:8765/callback/pINoQlIlx4_k`. Utiliser le callback effectivement annoncé par le client ; ne pas supposer ce suffixe universel.
- Scope technique demandé par Codex : `openid`. Aucun scope métier n'est ajouté au MCP.
- Le mapping d'audience sélectionné sur le fournisseur doit avoir **Scope name = `openid`**, avec l'expression suivante :

```python
return {
    "aud": [
        "om3-mcp",
        "https://dev5-vip.opensvc.com:8443/mcp",
    ],
}
```

Un login réussi ne suffisait pas : le MCP rejetait le token avec `audience_mismatch`, alors que la prévisualisation du fournisseur affichait la bonne audience. Le scope du mapping ne correspondait pas au scope demandé. Après correction en `openid` et nouveau login, Codex a découvert les outils du MCP. Les logs temporaires ayant permis ce diagnostic ont été retirés du code à la clôture de l'étape.

La recette réelle valide la connexion et la découverte des outils, sans JWT daemon fourni à Codex. Aucun appel métier n'a été demandé lors de cette recette. Le serveur bloque encore tous les `tools/call` avec un message explicite tant que l'échange de tokens n'est pas implémenté ; ce comportement est couvert par les tests locaux. Le middleware historique est conservé dans les sources, avec son branchement de production commenté. `om ai` et la webapp restent à adapter.

Restent à valider : renouvellement et révocation, autres clients externes, Keycloak, puis échange RFC 8693 et accès daemon. La présence de `resource` dans le parcours ne prouve pas sa prise en charge native par authentik : l'audience est configurée explicitement par le mapping.

### 3. Échanger le token pour accéder au daemon — prochain chantier, non commencé

- [ ] Fixer le contrat du profil SSO et vérifier la prise en charge RFC 8693 de la version déployée avant l'implémentation.
- [ ] Configurer l'identité confidentielle du MCP, distincte du client public de connexion, et les relations de confiance autorisant l'échange vers les audiences daemon.
- [ ] Choisir un daemon de lab et aligner son issuer et son audience avec le SSO de test : les fournisseurs daemon existants sur `auth.opensvc.com` ne sont pas automatiquement utilisables avec `labauthentik.opensvc.com`.
- [ ] Implémenter le client RFC 8693 avec authentification confidentielle du MCP.
- [ ] Résoudre audience et paramètres d'échange depuis une configuration administrée, jamais depuis une URL ou un issuer fourni par le LLM.
- [ ] Configurer un token aval accepté par le daemon : issuer, audience, signature, identité et droits OpenSVC attendus.
- [ ] Vérifier les mappings SSO produisant les grants OpenSVC de l'utilisateur pour le daemon cible, sans réécriture ni élargissement implicite par le MCP.
- [ ] Garder credentials et tokens hors des entrées/sorties d'outils, erreurs, logs et contexte LLM.
- [ ] Préserver l'identité de l'utilisateur et tracer l'application cliente, le cluster, l'outil et le résultat de l'échange et de l'appel daemon.
- [ ] Si un cache de tokens est nécessaire, l'isoler par identité, délégation, cible et permissions, avec une expiration bornée.

### 4. Découvrir et résoudre les cibles

- [ ] Implémenter et valider le nouveau catalogue : identifiants uniques, URLs HTTPS, profils connus et limites explicites.
- [ ] Revoir les limites actuelles du chargeur : 64 clusters et 256 Kio ne constituent pas une base adaptée à la cible annoncée.
- [ ] Implémenter `list_clusters` avec recherche et pagination sur tous les clusters configurés, sans filtrage par autorisation.
- [ ] Exiger `cluster_id` dans les outils métier de ce nouveau parcours ; exiger `node` pour ceux dont l'opération le nécessite.
- [ ] Résoudre `cluster_id → endpoint + profil + audience` indépendamment du nodename.
- [ ] Valider le token et la cible de chaque appel ; transmettre les refus du SSO ou du daemon, y compris si le client fournit directement un UUID.
- [ ] Garder les credentials et la cible dans un contexte propre à chaque appel, sans état utilisateur partagé.
- [ ] Définir les erreurs : cluster inaccessible ou inconnu, nom ambigu, nœud absent, échange refusé, daemon indisponible.

### 5. Valider authentik et Keycloak

- [ ] Établir une configuration reproductible authentik : clients, relations de confiance, audience et mappings.
- [ ] Établir une configuration reproductible Keycloak : clients, audience du token entrant, échange standard, scopes, mappings et restrictions.
- [ ] Vérifier pour chaque produit les deux étapes : connexion OAuth au MCP et échange vers le daemon.
- [ ] Documenter les versions testées, fonctionnalités requises et éventuelles différences de configuration.
- [ ] Documenter le contrat permettant d'intégrer un autre serveur d'autorisation ; ne pas annoncer une compatibilité universelle non testée.

### 6. Recette de bout en bout

- [x] Connecter Codex avec OAuth, sans lui fournir de JWT daemon ; découverte des outils validée au lab.
- [ ] Découvrir tous les clusters configurés avec un token MCP valide, y compris ceux dont une opération sera ensuite refusée par le SSO ou le daemon.
- [ ] Lire dev5n1/dev5 et dev4n1/dev4 avec le même token MCP.
- [ ] Vérifier les clarifications du LLM lorsque le cluster ou le nœud requis manque, ou que le nom du cluster est ambigu.
- [ ] Vérifier le cas de deux nœuds homonymes dans deux clusters.
- [ ] Vérifier la propagation d'un refus d'échange ou d'un refus de grants du daemon, même si l'UUID est fourni directement.
- [ ] Vérifier que tous les outils actuels sont exposés et que les grants du token aval déterminent leur exécution au daemon, sans scope métier MCP.
- [ ] Refuser un token expiré, altéré ou destiné à une autre ressource.
- [ ] Vérifier que le token MCP n'est pas accepté directement par le daemon et que le client externe ne peut pas effectuer l'échange réservé au MCP.
- [ ] Vérifier que les appels simultanés vers plusieurs clusters ne mélangent ni identité, ni cible, ni token.
- [ ] Restituer les succès partiels si l'un des clusters est indisponible, sans masquer l'échec ni changer de cible.
- [ ] Vérifier l'absence de credentials dans les sorties et journaux.
- [ ] Évaluer la découverte et le routage avec un catalogue représentatif de 800 clusters.

## V2 — Restrictions à réexaminer

Ces points sont des pistes à discuter après une V1 fonctionnelle, pas des prérequis ni des décisions déjà retenues pour la V2.

- [ ] Évaluer des scopes métier MCP pour distinguer les catégories d'opérations et limiter les outils accessibles à un agent externe.
- [ ] Évaluer un périmètre délégué plus restreint que les grants habituels de l'utilisateur : application cliente, clusters, namespaces, nœuds et opérations.
- [ ] Choisir où administrer ces restrictions : SSO, politique locale au MCP ou combinaison des deux.
- [ ] Évaluer un filtrage de `list_clusters` selon les droits, avec une source d'autorisation fiable et sans interrogation systématique des 800 clusters.
- [ ] Définir, si nécessaire, un consentement permettant à l'utilisateur de sélectionner un sous-ensemble de ses droits pour l'agent externe.
- [ ] Évaluer la réduction des privilèges des tokens aval et l'effet d'une modification ou révocation de délégation sur les tokens déjà émis.
- [ ] Si une politique locale est retenue, définir son stockage, sa mise à jour et son comportement à grande échelle.

## Autres points reportés

- Intégration ou migration de `om ai`, de la webapp et de leur parcours via `ai-agent`.
- Remplacement du YAML par Collector, CMDB ou inventaire synchronisé.
- Recherche d'un cluster à partir d'un nodename seul.
- Traitement détaillé du daemon portant la VIP et du routage vers le nœud d'exécution.
- Ajout de nouveaux outils au-delà de ceux actuellement exposés et de `list_clusters`.

## Critère de réussite

Un opérateur connecte son agent externe à `https://dev5-vip.opensvc.com:8443/mcp` via le SSO, découvre les clusters configurés, demande le statut de nœuds de deux clusters explicitement nommés et obtient une réponse fondée sur les résultats des outils. Tous les outils actuels restent exposés. Le MCP valide le token entrant et choisit les destinations ; le SSO délivre les grants et les daemons les appliquent. L'agent externe ne reçoit aucun token accepté par les daemons.

## Références

- [Autorisation MCP](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [Métadonnées de ressource protégée — RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html)
- [OAuth Token Exchange — RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html)
- [Token exchange authentik](https://docs.goauthentik.io/add-secure-apps/providers/oauth2/token_exchange/)
- [Token exchange Keycloak](https://www.keycloak.org/securing-apps/token-exchange)
