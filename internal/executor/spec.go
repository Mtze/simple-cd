// Package executor runs one deploy or teardown inside a Kubernetes Job. It
// fetches the target's secrets from Vault itself, so secrets never pass
// through the server or etcd.
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
