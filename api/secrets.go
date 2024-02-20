package api

import (
	"context"
	"net/url"
)

type GetSecretRequest struct {
	Key string
}

type Secret struct {
	Key   string `json:"name"`
	Value string `json:"value"`
	UUID  string `json:"uuid"`
}

func (c *Client) GetSecret(ctx context.Context, req *GetSecretRequest) (*Secret, *Response, error) {
	u := url.URL{Path: "secrets"}
	u.Query().Add("key", req.Key)

	httpReq, err := c.newRequest(ctx, "GET", railsPathEscape(u.String()), nil)
	if err != nil {
		return nil, nil, err
	}

	secret := &Secret{}
	resp, err := c.doRequest(httpReq, secret)
	if err != nil {
		return nil, resp, err
	}

	return secret, resp, nil
}