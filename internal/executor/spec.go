// Package executor runs one deploy or teardown inside a Kubernetes Job. It
// fetches the target's secrets from Vault itself, so secrets never pass
// through the server or etcd.
//
// Environment:
//
//	SIMPLECD_SPEC              the Spec as JSON (read by cmd/simple-cd-executor)
//	VAULT_ADDR                 Vault base URL
//	VAULT_TOKEN                token for local dev; without it Kubernetes auth is used
//	VAULT_NAMESPACE            optional Vault namespace
//	SIMPLECD_VAULT_ROLE        role for Kubernetes auth
//	SIMPLECD_VAULT_AUTH_MOUNT  auth mount, default "kubernetes"
//	SIMPLECD_SA_TOKEN_FILE     service account JWT, default /var/run/secrets/kubernetes.io/serviceaccount/token
//	SIMPLECD_SECRETS_DIR       where secret material is written (mode 0600), default /secrets (an in-memory emptyDir)
//	SIMPLECD_REGISTRY_PLAIN_HTTP  comma-separated registry hosts helm reaches over http (local test registries only)
//
// Secret references such as "secret/cd/clusters/test" are KV v2 paths whose
// first segment is the mount. Keys the executor expects:
//
//	cluster secret       kubeconfig
//	values secret        values (a YAML document)
//	registry secret      username, password
//	ssh secret (compose) private_key, known_hosts, user (optional, default "deploy")
//	ansible credentials  private_key, known_hosts (optional, required for ssh git URLs), user (optional)
package executor

// Spec is everything the executor needs, minus secrets. Secret fields are
// Vault paths (without the "vault:" prefix).
type Spec struct {
	DeploymentID int64        `json:"deploymentId"`
	Kind         string       `json:"kind"` // deploy or teardown
	Type         string       `json:"type"` // helm, compose or ansible
	TargetID     string       `json:"targetId"`
	Repo         string       `json:"repo"`
	SHA          string       `json:"sha,omitempty"`
	Helm         *HelmSpec    `json:"helm,omitempty"`
	Compose      *ComposeSpec `json:"compose,omitempty"`
	Ansible      *AnsibleSpec `json:"ansible,omitempty"`
}

type HelmSpec struct {
	ClusterSecret  string            `json:"clusterSecret"`
	Namespace      string            `json:"namespace"`
	Release        string            `json:"release"`
	Preview        bool              `json:"preview,omitempty"` // create the namespace with Labels, delete it on teardown
	Labels         map[string]string `json:"labels,omitempty"`
	Chart          string            `json:"chart,omitempty"`
	Version        string            `json:"version,omitempty"`
	Values         map[string]any    `json:"values,omitempty"`       // repo values merged with inline target values
	ValuesSecret   string            `json:"valuesSecret,omitempty"` // merged after Values
	Set            map[string]any    `json:"set,omitempty"`          // rendered helm.set, merged last
	RegistrySecret string            `json:"registrySecret,omitempty"`
	Timeout        string            `json:"timeout,omitempty"`
}

type ComposeSpec struct {
	Host        string            `json:"host"`
	Dir         string            `json:"dir"`
	SSHSecret   string            `json:"sshSecret"`
	ComposeFile string            `json:"composeFile"` // file content
	Env         map[string]string `json:"env"`         // written to .cd.env
}

type AnsibleSpec struct {
	RepoURL           string         `json:"repoUrl"` // git URL to clone
	Ref               string         `json:"ref"`     // branch, tag or SHA
	Playbook          string         `json:"playbook"`
	Inventory         string         `json:"inventory"`
	CredentialsSecret string         `json:"credentialsSecret"`
	ExtraVars         map[string]any `json:"extraVars"`
}
