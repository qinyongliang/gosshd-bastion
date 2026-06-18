package server

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd/internal/agent"
	"github.com/qinyongliang/gosshd/internal/store"

	gossh "golang.org/x/crypto/ssh"
)

func TestBastionE2E(t *testing.T) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp(Config{
		DatabasePath:      filepath.Join(t.TempDir(), "gosshd.db"),
		HostKeyPath:       filepath.Join(t.TempDir(), "host_key"),
		SessionCookieName: "e2e_session",
	})
	go func() {
		if err := app.RunListeners(ctx, httpLn, sshLn); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		if app.store != nil {
			_ = app.store.Close()
		}
	})

	baseURL := "http://" + httpLn.Addr().String()
	client := apiClient(t)
	user := registerForAPI(t, client, baseURL, "alice@example.com")

	var me apiMeResponse
	getJSON(t, client, baseURL+"/api/me", http.StatusOK, &me)
	if len(me.Organizations) != 1 || !me.Organizations[0].IsPersonal {
		t.Fatalf("personal organization missing: %+v", me.Organizations)
	}
	personalOrg := me.Organizations[0]

	var groups apiUserGroupsResponse
	getJSON(t, client, baseURL+"/api/orgs/"+personalOrg.ID+"/groups", http.StatusOK, &groups)
	if len(groups.Groups) != 1 || !groups.Groups[0].IsDefault {
		t.Fatalf("default group missing: %+v", groups)
	}
	var prompts apiLLMPromptsResponse
	getJSON(t, client, baseURL+"/api/llm-prompts?owner_type=organization&owner_id="+personalOrg.ID, http.StatusOK, &prompts)
	if len(prompts.Prompts) != 1 || !prompts.Prompts[0].IsReadonly {
		t.Fatalf("default prompt missing: %+v", prompts)
	}

	userSigner := testSSHSigner(t)
	var key apiPublicKeyResponse
	postJSON(t, client, baseURL+"/api/keys", map[string]string{
		"name":           "e2e",
		"authorized_key": string(gossh.MarshalAuthorizedKey(userSigner.PublicKey())),
	}, http.StatusCreated, &key)
	if key.Key.Fingerprint == "" {
		t.Fatalf("key response missing fingerprint")
	}

	targetAddr, closeTarget := startTestSSHServer(t)
	defer closeTarget()
	host, portText, _ := net.SplitHostPort(targetAddr)
	port := mustAtoi(t, portText)
	var target apiTargetResponse
	postJSON(t, client, baseURL+"/api/targets", map[string]any{
		"owner_type":      "organization",
		"owner_id":        personalOrg.ID,
		"alias":           "test2",
		"target_type":     "direct",
		"host":            host,
		"port":            port,
		"remote_username": "remote",
		"auth_type":       "password",
	}, http.StatusCreated, &target)

	out, err := runBastionSSHCommand(sshLn.Addr().String(), "test2", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected ssh output %q", out)
	}
	assertAuditContains(t, client, baseURL, "whoami", store.DecisionAllow)

	var policy apiPolicyResponse
	postJSON(t, client, baseURL+"/api/policies", map[string]any{
		"owner_type":     "organization",
		"owner_id":       personalOrg.ID,
		"name":           "deny destructive",
		"default_action": "allow",
	}, http.StatusCreated, &policy)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/rules", map[string]string{
		"rule_type":    "blacklist",
		"pattern_type": "contains",
		"pattern":      "rm -rf",
	}, http.StatusCreated, nil)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/targets", map[string]string{
		"target_id": target.Target.ID,
	}, http.StatusOK, nil)
	postJSON(t, client, baseURL+"/api/policies/"+policy.Policy.ID+"/user-groups", map[string]string{
		"group_id": groups.Groups[0].ID,
	}, http.StatusOK, nil)

	if _, err := runBastionSSHCommand(sshLn.Addr().String(), "test2", userSigner, "rm -rf /tmp/e2e"); err == nil {
		t.Fatalf("expected blacklist denial")
	}
	assertAuditContains(t, client, baseURL, "rm -rf /tmp/e2e", store.DecisionDeny)

	var enrollment apiAgentEnrollmentResponse
	postJSON(t, client, baseURL+"/api/agent-enrollments", map[string]any{
		"owner_type":   "organization",
		"owner_id":     personalOrg.ID,
		"label":        "agentbox-initial",
		"default_host": host,
		"default_port": port,
	}, http.StatusCreated, &enrollment)
	agentClient, err := agent.New(agent.Config{
		Server:          baseURL,
		EnrollmentToken: enrollment.Token,
		IDFile:          filepath.Join(t.TempDir(), "agent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()
	go func() {
		if err := agentClient.Run(agentCtx); err != nil {
			t.Logf("agent stopped: %v", err)
		}
	}()
	agentTarget := waitForAgentTarget(t, app, personalOrg.ID)
	var renamed apiTargetResponse
	patchJSON(t, client, baseURL+"/api/targets/"+agentTarget.ID, map[string]string{"alias": "agentbox"}, http.StatusOK, &renamed)
	out, err = runBastionSSHCommand(sshLn.Addr().String(), "agentbox", userSigner, "whoami")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "remote" {
		t.Fatalf("unexpected agent ssh output %q", out)
	}

	assertMCPListsTools(t, baseURL, http.DefaultClient)

	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "gosshd Bastion") {
		t.Fatalf("frontend response mismatch: %d %s", resp.StatusCode, body)
	}

	_ = user
}

func assertAuditContains(t *testing.T, client *http.Client, baseURL, command, decision string) {
	t.Helper()
	var logs apiAuditLogsResponse
	getJSON(t, client, baseURL+"/api/audit", http.StatusOK, &logs)
	for _, log := range logs.Logs {
		if log.Command == command && log.PolicyDecision == decision {
			return
		}
	}
	t.Fatalf("audit missing %q/%q in %+v", command, decision, logs.Logs)
}

func waitForAgentTarget(t *testing.T, app *App, orgID string) store.SSHTarget {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		targets, err := app.store.Repository().ListSSHTargets(context.Background(), store.OwnerOrganization, orgID)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			if target.TargetType == store.TargetAgent {
				return target
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent target not created")
	return store.SSHTarget{}
}

func assertMCPListsTools(t *testing.T, baseURL string, httpClient *http.Client) {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "gosshd-e2e"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             baseURL + "/mcp",
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpHasTool(tools, "target_create") {
		t.Fatalf("MCP target_create missing")
	}
}
