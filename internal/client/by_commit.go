package client

import (
	"fmt"
	"net/http"
	"net/url"
)

// ByCommitResponse is returned by GET /v1/cli/submissions/by-commit.
// The CLI polls this endpoint after a git push to discover the submission ID
// before moving on to trigger-token + SSE log streaming.
type ByCommitResponse struct {
	SubmissionID string `json:"submissionId"`
	StageSlug    string `json:"stageSlug"`
	StageName    string `json:"stageName"`
}

// GetSubmissionByCommit queries for a submission by commit SHA + course + language.
// Returns (*ByCommitResponse, nil) on success, or an *APIError with StatusCode 404
// when the submission has not yet been created (caller should retry).
func (c *Client) GetSubmissionByCommit(commitSha, courseSlug, languageSlug string) (*ByCommitResponse, error) {
	params := url.Values{}
	params.Set("commit_sha", commitSha)
	params.Set("course_slug", courseSlug)
	params.Set("language_slug", languageSlug)

	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/v1/cli/submissions/by-commit?%s", c.BaseURL, params.Encode()),
		nil,
	)
	if err != nil {
		return nil, err
	}

	var resp ByCommitResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
