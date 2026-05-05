package client

import (
	"fmt"
	"net/http"
)

// StageItem is a single stage returned inside CourseDetailResponse.
type StageItem struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// CourseDetailResponse is the relevant subset of GET /v1/courses/{slug}.
type CourseDetailResponse struct {
	Slug            string      `json:"slug"`
	ProgressionMode string      `json:"progressionMode"`
	Stages          []StageItem `json:"stages"`
}

// GetCourse fetches course detail including stage slugs.
// Used to validate --stage before committing or pushing.
func (c *Client) GetCourse(courseSlug string) (*CourseDetailResponse, error) {
	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/v1/courses/%s", c.BaseURL, courseSlug),
		nil,
	)
	if err != nil {
		return nil, err
	}

	var detail CourseDetailResponse
	if err := c.doJSON(req, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}
