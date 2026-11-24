package social

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

var linkedInPostID = regexp.MustCompile(`^urn:li:(share|ugcPost):[0-9]{1,32}$`)

func (p *remote) verifyLinkedInProfile(ctx context.Context, token string) (Account, error) {
	var profile struct {
		Subject string `json:"sub"`
		Name    string `json:"name"`
	}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.origin + "/v2/userinfo", token: token}, &profile); err != nil {
		return Account{}, err
	}
	if !remoteID.MatchString(profile.Subject) || !validAccountName(profile.Name) {
		return Account{}, ErrUnavailable
	}
	return Account{ID: "urn:li:person:" + profile.Subject, Name: profile.Name}, nil
}

func (p *remote) verifyLinkedInPage(ctx context.Context, token string) (Account, error) {
	target := "urn:li:organization:" + p.config.TargetID
	const count = 100
	for start := 0; start < 5*count; start += count {
		q := url.Values{"q": {"roleAssignee"}, "state": {"APPROVED"}, "count": {strconv.Itoa(count)}, "start": {strconv.Itoa(start)}}
		var acl struct {
			Elements []struct{ Organization, OrganizationTarget, Role, State string }
		}
		req := remoteRequest{method: http.MethodGet, endpoint: p.origin + "/rest/organizationAcls", token: token, query: q, linkedin: true}
		if _, err := p.request(ctx, req, &acl); err != nil {
			return Account{}, err
		}
		for _, role := range acl.Elements {
			if role.State != "APPROVED" || role.Organization != target && role.OrganizationTarget != target {
				continue
			}
			switch role.Role {
			case "ADMINISTRATOR", "CONTENT_ADMINISTRATOR", "CONTENT_ADMIN":
				return p.linkedInPageDetails(ctx, token, target)
			}
		}
		if len(acl.Elements) < count {
			break
		}
	}
	return Account{}, &providerError{status: http.StatusForbidden}
}

func (p *remote) linkedInPageDetails(ctx context.Context, token, urn string) (Account, error) {
	var out struct{ LocalizedName string }
	req := remoteRequest{method: http.MethodGet, endpoint: p.origin + "/rest/organizations/" + p.config.TargetID, token: token, linkedin: true}
	if _, err := p.request(ctx, req, &out); err != nil {
		return Account{}, err
	}
	if !validAccountName(out.LocalizedName) {
		return Account{}, ErrUnavailable
	}
	return Account{ID: urn, Name: out.LocalizedName, URL: "https://www.linkedin.com/company/" + p.config.TargetID}, nil
}

func (p *remote) sendLinkedIn(ctx context.Context, token string, post Message) (Receipt, error) {
	commentary := textWithoutLink(post)
	req := remoteRequest{method: http.MethodPost, token: token, linkedin: true}
	if p.key == linkedinPageKey {
		if post.AccountID != "urn:li:organization:"+p.config.TargetID {
			return Receipt{}, &providerError{status: http.StatusForbidden}
		}
		req.endpoint = p.origin + "/rest/posts"
		req.payload = map[string]any{
			"author": post.AccountID, "commentary": commentary, "visibility": "PUBLIC", "lifecycleState": "PUBLISHED",
			"distribution": map[string]any{"feedDistribution": "MAIN_FEED", "targetEntities": []string{}, "thirdPartyDistributionChannels": []string{}},
			"content":      map[string]any{"article": map[string]string{"source": post.Link, "title": post.Title}},
		}
	} else {
		req.endpoint = p.origin + "/v2/ugcPosts"
		req.payload = map[string]any{
			"author": post.AccountID, "lifecycleState": "PUBLISHED",
			"specificContent": map[string]any{"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]string{"text": commentary}, "shareMediaCategory": "ARTICLE",
				"media": []map[string]string{{"status": "READY", "originalUrl": post.Link}},
			}},
			"visibility": map[string]string{"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC"},
		}
	}
	headers, err := p.request(ctx, req, nil)
	if err != nil {
		return Receipt{}, err
	}
	id := headers.Get("X-RestLi-Id")
	if !linkedInPostID.MatchString(id) {
		return Receipt{}, &providerError{}
	}
	return receipt(id, "https://www.linkedin.com/feed/update/"+id+"/", post.AccountID), nil
}
