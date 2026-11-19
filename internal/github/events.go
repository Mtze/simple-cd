package github

// The subset of webhook payloads simple-cd reads.

type Repository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	SSHURL   string `json:"ssh_url"`
}

type Installation struct {
	ID int64 `json:"id"`
}

type User struct {
	Login string `json:"login"`
}

type WorkflowRunEvent struct {
	Action       string       `json:"action"`
	Repository   Repository   `json:"repository"`
	Installation Installation `json:"installation"`
	WorkflowRun  struct {
		ID              int64      `json:"id"`
		Name            string     `json:"name"`
		Path            string     `json:"path"`
		Event           string     `json:"event"`
		Status          string     `json:"status"`
		Conclusion      string     `json:"conclusion"`
		HeadBranch      string     `json:"head_branch"`
		HeadSHA         string     `json:"head_sha"`
		HTMLURL         string     `json:"html_url"`
		HeadRepository  Repository `json:"head_repository"`
		TriggeringActor User       `json:"triggering_actor"`
		PullRequests    []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
}

type PullRequestEvent struct {
	Action       string       `json:"action"`
	Number       int          `json:"number"`
	Repository   Repository   `json:"repository"`
	Installation Installation `json:"installation"`
}
