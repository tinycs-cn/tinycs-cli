package client

import (
	"fmt"
	"net/http"
	"net/url"
)

// GetCurrentStage calls GET /v1/current-stage?course=<slug>&language=<slug>.
// Returns the current stage for sequential courses.
// Returns *APIError with StatusCode 400 for freeform courses or missing params.
// Returns *APIError with StatusCode 404 if the repo is not registered or no stage is assigned.
func (c *Client) GetCurrentStage(courseSlug, languageSlug string) (*StageItem, error) {
	u := fmt.Sprintf("%s/v1/current-stage", c.BaseURL)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("course", courseSlug)
	q.Set("language", languageSlug)
	req.URL.RawQuery = q.Encode()

	var item StageItem
	if err := c.doJSON(req, &item); err != nil {
		return nil, err
	}
	return &item, nil
}
