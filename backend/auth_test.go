package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clerkReqWithOrg returns a request with Clerk session claims that include
// the given orgID and orgRole (e.g. "org:admin" or "org:member").
func clerkReqWithOrg(orgID, orgRole string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user_test"},
		Claims: clerk.Claims{
			ActiveOrganizationID:   orgID,
			ActiveOrganizationRole: orgRole,
		},
	})
	return req.WithContext(ctx)
}

// clerkReqNoOrg returns a request with Clerk session claims but no active org.
func clerkReqNoOrg() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	ctx := clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{Subject: "user_test"},
	})
	return req.WithContext(ctx)
}

// clerkReqNoClaims returns a request with no Clerk session claims in context.
func clerkReqNoClaims() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
}

// --- groupIDFromRequest ---

func TestGroupIDFromRequest_ReturnsOrgID(t *testing.T) {
	req := clerkReqWithOrg("org_abc123", "org:member")
	id, err := groupIDFromRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "org_abc123", id)
}

func TestGroupIDFromRequest_EmptyOrgID_ReturnsNoActiveOrg(t *testing.T) {
	req := clerkReqNoOrg()
	_, err := groupIDFromRequest(req)
	require.Error(t, err)
	var ae *apiError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, http.StatusForbidden, ae.Status)
	assert.Equal(t, "no_active_org", ae.Code)
}

func TestGroupIDFromRequest_NoClaims_ReturnsUnauthorized(t *testing.T) {
	req := clerkReqNoClaims()
	_, err := groupIDFromRequest(req)
	require.Error(t, err)
	var ae *apiError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, http.StatusUnauthorized, ae.Status)
	assert.Equal(t, "unauthorized", ae.Code)
}

// --- isAdmin ---

func TestIsAdmin_AdminRole_ReturnsTrue(t *testing.T) {
	req := clerkReqWithOrg("org_abc123", "org:admin")
	assert.True(t, isAdmin(req))
}

func TestIsAdmin_MemberRole_ReturnsFalse(t *testing.T) {
	req := clerkReqWithOrg("org_abc123", "org:member")
	assert.False(t, isAdmin(req))
}

func TestIsAdmin_NoClaims_ReturnsFalse(t *testing.T) {
	req := clerkReqNoClaims()
	assert.False(t, isAdmin(req))
}
