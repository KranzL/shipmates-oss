package github

import "time"

type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	DiffURL   string    `json:"diff_url"`
	Head      PRRef     `json:"head"`
	Base      PRRef     `json:"base"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PRRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type User struct {
	Login string `json:"login"`
}

type ReviewComment struct {
	Path     string `json:"path"`
	Position int    `json:"position,omitempty"`
	Line     int    `json:"line,omitempty"`
	Side     string `json:"side,omitempty"`
	Body     string `json:"body"`
}

type Review struct {
	Body     string          `json:"body"`
	Event    string          `json:"event"`
	Comments []ReviewComment `json:"comments,omitempty"`
}

type WorkflowRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	HeadSHA      string    `json:"head_sha"`
	HeadBranch   string    `json:"head_branch"`
	RunNumber    int       `json:"run_number"`
	LogsURL      string    `json:"logs_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	PullRequests []struct {
		Number int `json:"number"`
	} `json:"pull_requests"`
}

type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  struct {
		Name  string    `json:"name"`
		Email string    `json:"email"`
		Date  time.Time `json:"date"`
	} `json:"author"`
	HTMLURL string `json:"html_url"`
}

type CommitResponse struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name  string    `json:"name"`
			Email string    `json:"email"`
			Date  time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	HTMLURL string `json:"html_url"`
}

type Repository struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

type CreatePRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

type CreatePRResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}
