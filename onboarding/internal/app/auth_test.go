package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	authadapter "avito-ai-buddy/onboarding/internal/adapter/auth/http"
	planpb "avito-ai-buddy/pkg/generated/plan/v1"
	steppb "avito-ai-buddy/pkg/generated/step/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type authTransportFunc func(*http.Request) (*http.Response, error)

func (f authTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAuthorizeRequests(t *testing.T) {
	const accessToken = "test-access-token"
	const serviceToken = "test-service-token"
	const validOwner = `{"is_valid":true,"user_id":7}`

	getPlan := planpb.PlanService_GetPlan_FullMethodName
	getPlanContext := planpb.PlanService_GetPlanContext_FullMethodName
	completeStep := steppb.StepService_CompleteStep_FullMethodName
	reopenStep := steppb.StepService_ReopenStep_FullMethodName
	updateTemplate := steppb.StepService_UpdateStepTemplate_FullMethodName

	for _, tc := range []struct {
		name       string
		method     string
		request    any
		token      string
		authStatus int
		authBody   string
		want       codes.Code
	}{
		{"assign own plan", planpb.PlanService_AssignPlan_FullMethodName, &planpb.AssignPlanRequest{UserId: 7}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"read own plan", getPlan, &planpb.GetPlanRequest{UserId: 7}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"read own plan context", getPlanContext, &planpb.GetPlanContextRequest{UserId: 7}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"cannot read another user's plan context", getPlanContext, &planpb.GetPlanContextRequest{UserId: 8}, accessToken, http.StatusOK, validOwner, codes.PermissionDenied},
		{"service token cannot read plan context", getPlanContext, &planpb.GetPlanContextRequest{UserId: 7}, serviceToken, 0, "", codes.Unauthenticated},
		{"read own current step", steppb.StepService_GetCurrentStep_FullMethodName, &steppb.GetCurrentStepRequest{UserId: 7}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"complete own step", completeStep, &steppb.CompleteStepRequest{UserId: 7}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"reopen own step", reopenStep, &steppb.ReopenStepRequest{UserId: 7, Status: "pending"}, accessToken, http.StatusOK, validOwner, codes.OK},
		{"cannot reopen another user's step", reopenStep, &steppb.ReopenStepRequest{UserId: 8, Status: "pending"}, accessToken, http.StatusOK, validOwner, codes.PermissionDenied},
		{"different owner", completeStep, &steppb.CompleteStepRequest{UserId: 8}, accessToken, http.StatusOK, validOwner, codes.PermissionDenied},
		{"missing token", getPlan, &planpb.GetPlanRequest{UserId: 7}, "", 0, "", codes.Unauthenticated},
		{"health check without token", healthpb.Health_Check_FullMethodName, &healthpb.HealthCheckRequest{}, "", 0, "", codes.OK},
		{"invalid token", getPlan, &planpb.GetPlanRequest{UserId: 7}, accessToken, http.StatusUnauthorized, "", codes.Unauthenticated},
		{"invalid identity from Auth", getPlan, &planpb.GetPlanRequest{UserId: 7}, accessToken, http.StatusOK, `{"is_valid":true,"user_id":0}`, codes.Unavailable},
		{"Auth unavailable", getPlan, &planpb.GetPlanRequest{UserId: 7}, accessToken, http.StatusServiceUnavailable, "", codes.Unavailable},
		{"service token without user session", completeStep, &steppb.CompleteStepRequest{UserId: 7}, serviceToken, 0, "", codes.Unauthenticated},
		{"service edits template", updateTemplate, &steppb.UpdateStepTemplateRequest{}, serviceToken, 0, "", codes.OK},
		{"user cannot edit template", updateTemplate, &steppb.UpdateStepTemplateRequest{}, accessToken, 0, "", codes.Unauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authCalls := 0
			client := &http.Client{Transport: authTransportFunc(func(req *http.Request) (*http.Response, error) {
				authCalls++
				if tc.authStatus == 0 {
					t.Fatal("unexpected Auth request")
				}
				if req.Method != http.MethodPost || req.URL.Path != "/v1/auth/validate" || req.Header.Get("Authorization") != "Bearer "+accessToken {
					t.Fatal("unexpected Auth request method, path or credentials")
				}
				return &http.Response{StatusCode: tc.authStatus, Body: io.NopCloser(strings.NewReader(tc.authBody))}, nil
			})}
			auth := authadapter.NewClient(client, "http://auth:8080", "/v1/auth/validate")
			ctx := t.Context()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+tc.token))
			}
			handled := false
			_, err := authorizeRequests(auth, serviceToken)(ctx, tc.request, &grpc.UnaryServerInfo{FullMethod: tc.method}, func(_ context.Context, _ any) (any, error) {
				handled = true
				return nil, nil
			})
			if got := status.Code(err); got != tc.want {
				t.Fatalf("authorization code = %s, want %s", got, tc.want)
			}
			if handled != (tc.want == codes.OK) {
				t.Fatalf("controller called = %t, authorization code = %s", handled, tc.want)
			}
			if tc.authStatus != 0 && authCalls != 1 {
				t.Fatalf("Auth calls = %d, want 1", authCalls)
			}
		})
	}
}
