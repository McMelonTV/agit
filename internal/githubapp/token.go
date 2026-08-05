package githubapp

import (
	"context"
	"time"
)

func (c *Client) CachedInstallationToken(ctx context.Context, installationID int64, cache TokenCache, refreshBefore time.Duration) (Token, error) {
	unlock, locked := cache.Lock(ctx, c.AppID, c.APIURL, installationID)
	if locked {
		defer unlock()
	} else if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	if token, ok, _ := cache.Get(c.AppID, c.APIURL, installationID, refreshBefore); ok {
		return token, nil
	}

	token, err := c.CreateInstallationToken(ctx, installationID)
	if err != nil {
		return Token{}, err
	}
	_ = cache.Put(c.AppID, c.APIURL, installationID, token)
	return token, nil
}
