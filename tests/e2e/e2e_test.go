package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zelayan/agent-monitor/internal/application/monitor"
	"github.com/Zelayan/agent-monitor/internal/domain/task"
	"github.com/Zelayan/agent-monitor/internal/infrastructure/persistence"
	transport "github.com/Zelayan/agent-monitor/internal/infrastructure/transport/http"
)

// findRepoRoot 沿目录树向上查找包含 go.mod 的项目根目录。
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repository root containing go.mod starting from %s", dir)
		}
		dir = parent
	}
}

// testServerOptions 封装测试服务器的可选配置。
type testServerOptions struct {
	repoCap     int
	ringCap     int
	apiKey      string
	masterKey   string
	projectKeys string
	customDir   string
}

// testServerInstance 封装运行中的测试服务器及关联组件。
type testServerInstance struct {
	URL        string
	Server     *httptest.Server
	Service    *monitor.MonitorService
	Hub        *monitor.Hub
	Repo       *persistence.JSONRepository
	DataDir    string
	RepoRoot   string
	StaticHTML []byte
	Cleanup    func()
}

// startTestServer 启动一个完全独立的、基于动态随机端口的 HTTP 测试服务器，绝不占用 :8000。
func startTestServer(t *testing.T, opts testServerOptions) *testServerInstance {
	t.Helper()
	repoRoot := findRepoRoot(t)

	dataDir := opts.customDir
	if dataDir == "" {
		var err error
		dataDir, err = os.MkdirTemp("", "agent-monitor-e2e-*")
		if err != nil {
			t.Fatalf("failed to create temp data dir: %v", err)
		}
	}

	repo, err := persistence.NewJSONRepository(dataDir)
	if err != nil {
		t.Fatalf("failed to initialize JSONRepository in %s: %v", dataDir, err)
	}

	hubCap := opts.ringCap
	if hubCap <= 0 {
		hubCap = 512
	}
	hub := monitor.NewHubWithCapacity(hubCap)
	go hub.Run()

	svc := monitor.NewMonitorService(repo, hub)

	indexHTML, err := os.ReadFile(filepath.Join(repoRoot, "static/index.html"))
	if err != nil {
		t.Fatalf("failed to read static/index.html: %v", err)
	}

	staticFS := os.DirFS(repoRoot)

	handler := transport.NewHandler(svc, hub, indexHTML).
		WithStaticFS(staticFS)

	if opts.apiKey != "" {
		handler.WithAPIKey(opts.apiKey)
	}
	if opts.masterKey != "" {
		handler.WithMasterKey(opts.masterKey)
	}
	if opts.projectKeys != "" {
		handler.WithProjectKeys(opts.projectKeys)
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	ts := httptest.NewServer(mux)

	cleanup := func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.CloseWithContext(ctx)
		_ = repo.Close()
		if opts.customDir == "" {
			_ = os.RemoveAll(dataDir)
		}
	}

	return &testServerInstance{
		URL:        ts.URL,
		Server:     ts,
		Service:    svc,
		Hub:        hub,
		Repo:       repo,
		DataDir:    dataDir,
		RepoRoot:   repoRoot,
		StaticHTML: indexHTML,
		Cleanup:    cleanup,
	}
}

// parsedSSEEvent 表示从 SSE 流中解析出的单条事件。
type parsedSSEEvent struct {
	ID    int64
	Event string
	Data  string
}

// readSSEEvent 从带有超时的 bufio.Reader 中读取下一个完整的 SSE 事件。
func readSSEEvent(reader *bufio.Reader, timeout time.Duration) (parsedSSEEvent, error) {
	type result struct {
		ev  parsedSSEEvent
		err error
	}
	ch := make(chan result, 1)

	go func() {
		var ev parsedSSEEvent
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				ch <- result{ev: ev, err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if ev.Event != "" || ev.Data != "" || ev.ID != 0 {
					ch <- result{ev: ev, err: nil}
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				// 心跳或注释行，跳过
				continue
			}
			if strings.HasPrefix(line, "id:") {
				fmt.Sscanf(strings.TrimSpace(line[3:]), "%d", &ev.ID)
			} else if strings.HasPrefix(line, "event:") {
				ev.Event = strings.TrimSpace(line[6:])
			} else if strings.HasPrefix(line, "data:") {
				ev.Data = strings.TrimSpace(line[5:])
			}
		}
	}()

	select {
	case res := <-ch:
		return res.ev, res.err
	case <-time.After(timeout):
		return parsedSSEEvent{}, fmt.Errorf("timeout waiting for SSE event after %v", timeout)
	}
}

// postHookEvent 向 /api/event 提交 Hook 事件并返回 HTTP 状态码及 JSON 响应。
func postHookEvent(t *testing.T, srvURL string, apiKey string, p task.EventPayload) (int, map[string]interface{}) {
	t.Helper()
	bodyBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srvURL+"/api/event", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute POST /api/event: %v", err)
	}
	defer resp.Body.Close()

	var res map[string]interface{}
	respBytes, _ := io.ReadAll(resp.Body)
	if len(respBytes) > 0 {
		_ = json.Unmarshal(respBytes, &res)
	}
	return resp.StatusCode, res
}

// fetchTasks 从 /api/tasks 查询任务列表。
func fetchTasks(t *testing.T, srvURL string, apiKey string) (int, []*task.Task) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srvURL+"/api/tasks", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute GET /api/tasks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}

	var list []*task.Task
	_ = json.NewDecoder(resp.Body).Decode(&list)
	return resp.StatusCode, list
}

// extractErrorCode 从统一的 APIErrorResponse ({"error":{"code":"..."}}) 或顶层结构中提取错误码。
func extractErrorCode(data map[string]interface{}) string {
	if errObj, ok := data["error"].(map[string]interface{}); ok {
		if code, ok := errObj["code"].(string); ok {
			return code
		}
	}
	if code, ok := data["code"].(string); ok {
		return code
	}
	return ""
}

// -----------------------------------------------------------------------------
// 1. Static Shell, Service Worker & PWA Manifest
// -----------------------------------------------------------------------------

func TestE2E_StaticShell_SW_And_Manifest(t *testing.T) {
	inst := startTestServer(t, testServerOptions{})
	defer inst.Cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// 1.1 GET / (Static Shell)
	resp, err := client.Get(inst.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected GET / 200 OK, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read GET / body: %v", err)
	}
	htmlStr := string(body)

	// 验证必要 PWA 与 A11y 标记
	requiredMarkers := []string{
		`role="dialog"`,
		`aria-live="polite"`,
		`aria-live="assertive"`,
		`:focus-visible`,
		`agent_monitor_offline_db`,
		`manifest.json`,
		`sw.js`,
	}
	for _, marker := range requiredMarkers {
		if !strings.Contains(htmlStr, marker) {
			t.Errorf("GET / missing essential PWA/A11y marker: %q", marker)
		}
	}

	// 1.2 GET /manifest.json (PWA Web App Manifest)
	respManifest, err := client.Get(inst.URL + "/manifest.json")
	if err != nil {
		t.Fatalf("GET /manifest.json failed: %v", err)
	}
	defer respManifest.Body.Close()

	if respManifest.StatusCode != http.StatusOK {
		t.Fatalf("expected GET /manifest.json 200, got %d", respManifest.StatusCode)
	}
	mct := respManifest.Header.Get("Content-Type")
	if !strings.Contains(mct, "application/manifest+json") && !strings.Contains(mct, "application/json") {
		t.Errorf("expected manifest Content-Type application/manifest+json, got %s", mct)
	}
	var manifestData map[string]interface{}
	if err := json.NewDecoder(respManifest.Body).Decode(&manifestData); err != nil {
		t.Fatalf("failed to parse manifest.json: %v", err)
	}
	if name, ok := manifestData["name"].(string); !ok || !strings.Contains(name, "AGENT MONITOR") {
		t.Errorf("manifest name mismatch, got %v", manifestData["name"])
	}
	if display, ok := manifestData["display"].(string); !ok || display != "standalone" {
		t.Errorf("manifest display expected standalone, got %v", manifestData["display"])
	}

	// 1.3 GET /sw.js (Service Worker)
	respSW, err := client.Get(inst.URL + "/sw.js")
	if err != nil {
		t.Fatalf("GET /sw.js failed: %v", err)
	}
	defer respSW.Body.Close()

	if respSW.StatusCode != http.StatusOK {
		t.Fatalf("expected GET /sw.js 200, got %d", respSW.StatusCode)
	}
	swBody, err := io.ReadAll(respSW.Body)
	if err != nil {
		t.Fatalf("failed to read /sw.js body: %v", err)
	}
	swStr := string(swBody)
	if !strings.Contains(swStr, "agent-monitor-") || !strings.Contains(swStr, "addEventListener") {
		t.Errorf("sw.js content invalid: %s", swStr)
	}

	// 1.4 GET /healthz
	respHealth, err := client.Get(inst.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer respHealth.Body.Close()
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("expected GET /healthz 200, got %d", respHealth.StatusCode)
	}
	var healthData map[string]interface{}
	_ = json.NewDecoder(respHealth.Body).Decode(&healthData)
	if healthData["status"] != "ok" {
		t.Errorf("healthz expected status ok, got %v", healthData)
	}

	// 1.5 GET /readyz
	respReady, err := client.Get(inst.URL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz failed: %v", err)
	}
	defer respReady.Body.Close()
	if respReady.StatusCode != http.StatusOK {
		t.Fatalf("expected GET /readyz 200, got %d", respReady.StatusCode)
	}
	var readyData map[string]interface{}
	_ = json.NewDecoder(respReady.Body).Decode(&readyData)
	if readyData["status"] != "ready" {
		t.Errorf("readyz expected status ready, got %v", readyData)
	}

	// 1.6 GET /api/metrics
	respMetrics, err := client.Get(inst.URL + "/api/metrics")
	if err != nil {
		t.Fatalf("GET /api/metrics failed: %v", err)
	}
	defer respMetrics.Body.Close()
	if respMetrics.StatusCode != http.StatusOK {
		t.Fatalf("expected GET /api/metrics 200, got %d", respMetrics.StatusCode)
	}
	var metricsData map[string]interface{}
	if err := json.NewDecoder(respMetrics.Body).Decode(&metricsData); err != nil {
		t.Fatalf("failed to decode metrics JSON: %v", err)
	}
	if _, ok := metricsData["total_events_received"]; !ok {
		t.Errorf("metrics JSON missing total_events_received: %v", metricsData)
	}
	if _, ok := metricsData["persist_queue_capacity"]; !ok {
		t.Errorf("metrics JSON missing persist_queue_capacity: %v", metricsData)
	}
}

// -----------------------------------------------------------------------------
// 2. SSE v2 Full-Cycle Reconnection & Snapshot Reconciliation
// -----------------------------------------------------------------------------

func TestE2E_SSE_FullCycle_Reconnection_And_Snapshot(t *testing.T) {
	// 使用小型环形缓冲区 (容量 5) 方便测试溢出与 resync_required
	inst := startTestServer(t, testServerOptions{ringCap: 5})
	defer inst.Cleanup()

	// 预置初始任务
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     "task-initial-1",
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Initial work session",
		Prompt: "Implement feature A",
	})

	// 2.1 客户端首次建立连接 (无 Last-Event-ID)，必须收到权威全量快照序列：
	// snapshot_start -> task_upsert -> snapshot_end
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, inst.URL+"/api/stream", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("failed to connect to SSE stream: %v", err)
	}
	defer resp1.Body.Close()

	reader1 := bufio.NewReader(resp1.Body)

	// 事件 1: snapshot_start
	evStart, err := readSSEEvent(reader1, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read snapshot_start: %v", err)
	}
	if evStart.Event != "snapshot_start" {
		t.Fatalf("expected snapshot_start event, got %q", evStart.Event)
	}

	// 事件 2: task_upsert (task-initial-1)
	evUpsert, err := readSSEEvent(reader1, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read initial task_upsert: %v", err)
	}
	if evUpsert.Event != "task_upsert" || !strings.Contains(evUpsert.Data, "task-initial-1") {
		t.Fatalf("expected task_upsert for task-initial-1, got %q, data: %s", evUpsert.Event, evUpsert.Data)
	}

	// 事件 3: snapshot_end
	evEnd, err := readSSEEvent(reader1, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read snapshot_end: %v", err)
	}
	if evEnd.Event != "snapshot_end" {
		t.Fatalf("expected snapshot_end event, got %q", evEnd.Event)
	}
	var endObj map[string]interface{}
	_ = json.Unmarshal([]byte(evEnd.Data), &endObj)
	if count, ok := endObj["count"].(float64); !ok || count < 1 {
		t.Fatalf("snapshot_end count expected >= 1, got %v", endObj["count"])
	}

	// 2.2 在连接保持状态下派发实时 Hook 事件 -> 验证单调递增 ID 与实时广播
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     "task-live-1",
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Live session start",
		Prompt: "Testing live SSE stream",
	})

	evLive, err := readSSEEvent(reader1, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read live event: %v", err)
	}
	if evLive.Event != "task_upsert" || !strings.Contains(evLive.Data, "task-live-1") {
		t.Fatalf("expected live task_upsert for task-live-1, got %q, data: %s", evLive.Event, evLive.Data)
	}
	if evLive.ID <= 0 {
		t.Fatalf("expected positive monotonic ID for live event, got %d", evLive.ID)
	}
	capturedLastID := evLive.ID

	// 断开连接 1
	cancel1()
	resp1.Body.Close()

	// 2.3 携带有效 Last-Event-ID 重连 -> 环形缓冲区仅重放错过的增量事件，绝无重复快照
	// 在断线期间产生 2 个新事件
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     "task-reconnect-1",
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Incremental 1",
		Prompt: "Work 1",
	})
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     "task-reconnect-2",
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Incremental 2",
		Prompt: "Work 2",
	})

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, inst.URL+"/api/stream", nil)
	req2.Header.Set("Last-Event-ID", fmt.Sprintf("%d", capturedLastID))

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("failed to reconnect SSE with Last-Event-ID: %v", err)
	}
	defer resp2.Body.Close()
	reader2 := bufio.NewReader(resp2.Body)

	// 重连后第 1 个事件应当是 task-reconnect-1，绝对不能是 snapshot_start
	evRec1, err := readSSEEvent(reader2, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read incremental event 1: %v", err)
	}
	if evRec1.Event == "snapshot_start" {
		t.Fatalf("unexpected snapshot_start on valid Last-Event-ID reconnection")
	}
	if evRec1.Event != "task_upsert" || !strings.Contains(evRec1.Data, "task-reconnect-1") {
		t.Fatalf("expected replayed task-reconnect-1, got %q (data: %s)", evRec1.Event, evRec1.Data)
	}
	if evRec1.ID <= capturedLastID {
		t.Fatalf("expected replayed event ID > %d, got %d", capturedLastID, evRec1.ID)
	}

	// 第 2 个事件应当是 task-reconnect-2
	evRec2, err := readSSEEvent(reader2, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read incremental event 2: %v", err)
	}
	if evRec2.Event != "task_upsert" || !strings.Contains(evRec2.Data, "task-reconnect-2") {
		t.Fatalf("expected replayed task-reconnect-2, got %q", evRec2.Event)
	}

	cancel2()
	resp2.Body.Close()

	// 2.4 携带过期/超界 Last-Event-ID 重连 -> 验证先下发 resync_required，随后完整快照对账
	// 往环形缓冲 (容量 5) 注入 10 个事件迫使其发生滚动覆盖
	for i := 0; i < 10; i++ {
		postHookEvent(t, inst.URL, "", task.EventPayload{
			ID:     fmt.Sprintf("task-overflow-%d", i),
			Agent:  "ZCode",
			Event:  "sessionStart",
			Detail: fmt.Sprintf("Overflow push %d", i),
			Prompt: "Burst work",
		})
	}

	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()

	req3, _ := http.NewRequestWithContext(ctx3, http.MethodGet, inst.URL+"/api/stream", nil)
	// 使用早期已被覆盖的 ID
	req3.Header.Set("Last-Event-ID", "1")

	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("failed to reconnect with expired Last-Event-ID: %v", err)
	}
	defer resp3.Body.Close()
	reader3 := bufio.NewReader(resp3.Body)

	// 必须首先收到 resync_required
	evResync, err := readSSEEvent(reader3, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read resync_required: %v", err)
	}
	if evResync.Event != "resync_required" {
		t.Fatalf("expected resync_required event for expired buffer, got %q (data: %s)", evResync.Event, evResync.Data)
	}
	var resyncObj map[string]interface{}
	_ = json.Unmarshal([]byte(evResync.Data), &resyncObj)
	if resyncObj["reason"] != "buffer_overflow" {
		t.Errorf("expected reason buffer_overflow, got %v", resyncObj["reason"])
	}

	// 随后紧跟着权威全量快照：snapshot_start
	evSnapshotAfterResync, err := readSSEEvent(reader3, 3*time.Second)
	if err != nil {
		t.Fatalf("failed to read snapshot_start after resync: %v", err)
	}
	if evSnapshotAfterResync.Event != "snapshot_start" {
		t.Fatalf("expected snapshot_start after resync_required, got %q", evSnapshotAfterResync.Event)
	}
}

// -----------------------------------------------------------------------------
// 3. Multi-Tenant API Key Isolation & Permissions
// -----------------------------------------------------------------------------

func TestE2E_MultiTenant_APIKey_Isolation_And_Permissions(t *testing.T) {
	inst := startTestServer(t, testServerOptions{
		masterKey:   "master-secret-key-999",
		projectKeys: "tenant-alpha=key-alpha,tenant-beta=key-beta",
	})
	defer inst.Cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// 3.1 未携带凭证请求受保护接口 -> 返回类型化 401 UNAUTHORIZED
	reqUnauth, _ := http.NewRequest(http.MethodGet, inst.URL+"/api/tasks", nil)
	respUnauth, err := client.Do(reqUnauth)
	if err != nil {
		t.Fatalf("failed to request unauthenticated /api/tasks: %v", err)
	}
	defer respUnauth.Body.Close()
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for missing API key, got %d", respUnauth.StatusCode)
	}
	var errDTO map[string]interface{}
	_ = json.NewDecoder(respUnauth.Body).Decode(&errDTO)
	if extractErrorCode(errDTO) != "UNAUTHORIZED" {
		t.Errorf("expected typed error code UNAUTHORIZED, got %v", errDTO)
	}

	// 3.2 两个租户使用完全相同的 Session ID 上报会话 -> 必须完全物理空间隔离
	sharedSessionID := "sess-multi-tenant-conflict-01"

	// Tenant Alpha 上报
	codeA, _ := postHookEvent(t, inst.URL, "key-alpha", task.EventPayload{
		ID:     sharedSessionID,
		Agent:  "Cursor",
		Event:  "sessionStart",
		Detail: "Alpha session workspace",
		Prompt: "Project Alpha prompt",
	})
	if codeA != http.StatusOK {
		t.Fatalf("expected 200 for tenant Alpha event, got %d", codeA)
	}

	// Tenant Beta 上报同名 Session
	codeB, _ := postHookEvent(t, inst.URL, "key-beta", task.EventPayload{
		ID:     sharedSessionID,
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Beta session workspace",
		Prompt: "Project Beta prompt",
	})
	if codeB != http.StatusOK {
		t.Fatalf("expected 200 for tenant Beta event, got %d", codeB)
	}

	// 查询 Tenant Alpha 任务列表 -> 仅见 Alpha 任务
	codeTasksA, tasksA := fetchTasks(t, inst.URL, "key-alpha")
	if codeTasksA != http.StatusOK || len(tasksA) != 1 {
		t.Fatalf("tenant Alpha expected 1 task, got %d (code %d)", len(tasksA), codeTasksA)
	}
	if tasksA[0].KeyID != "tenant-alpha" || tasksA[0].Prompt != "Project Alpha prompt" {
		t.Errorf("tenant Alpha task data mismatch: %+v", tasksA[0])
	}

	// 查询 Tenant Beta 任务列表 -> 仅见 Beta 任务
	codeTasksB, tasksB := fetchTasks(t, inst.URL, "key-beta")
	if codeTasksB != http.StatusOK || len(tasksB) != 1 {
		t.Fatalf("tenant Beta expected 1 task, got %d (code %d)", len(tasksB), codeTasksB)
	}
	if tasksB[0].KeyID != "tenant-beta" || tasksB[0].Prompt != "Project Beta prompt" {
		t.Errorf("tenant Beta task data mismatch: %+v", tasksB[0])
	}

	// Master Key 查询 -> 同时感知两个租户任务 (全局视图)
	codeTasksMaster, tasksMaster := fetchTasks(t, inst.URL, "master-secret-key-999")
	if codeTasksMaster != http.StatusOK || len(tasksMaster) != 2 {
		t.Fatalf("master key expected 2 tasks from both tenants, got %d", len(tasksMaster))
	}

	// 3.3 租户级 SSE 实时事件流隔离验证
	// 客户端 Alpha 连接 Alpha 流
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	reqStreamA, _ := http.NewRequestWithContext(ctxA, http.MethodGet, inst.URL+"/api/stream?api_key=key-alpha", nil)
	respStreamA, err := client.Do(reqStreamA)
	if err != nil {
		t.Fatalf("failed to connect SSE for tenant Alpha: %v", err)
	}
	defer respStreamA.Body.Close()
	readerStreamA := bufio.NewReader(respStreamA.Body)

	// 客户端 Beta 连接 Beta 流
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	reqStreamB, _ := http.NewRequestWithContext(ctxB, http.MethodGet, inst.URL+"/api/stream?api_key=key-beta", nil)
	respStreamB, err := client.Do(reqStreamB)
	if err != nil {
		t.Fatalf("failed to connect SSE for tenant Beta: %v", err)
	}
	defer respStreamB.Body.Close()
	readerStreamB := bufio.NewReader(respStreamB.Body)

	// 先消费完各自的初始快照
	_, _ = readSSEEvent(readerStreamA, 3*time.Second) // start
	_, _ = readSSEEvent(readerStreamA, 3*time.Second) // upsert
	_, _ = readSSEEvent(readerStreamA, 3*time.Second) // end

	_, _ = readSSEEvent(readerStreamB, 3*time.Second) // start
	_, _ = readSSEEvent(readerStreamB, 3*time.Second) // upsert
	_, _ = readSSEEvent(readerStreamB, 3*time.Second) // end

	// 向 Tenant Alpha 发送新事件
	postHookEvent(t, inst.URL, "key-alpha", task.EventPayload{
		ID:     "sess-alpha-private",
		Agent:  "Cursor",
		Event:  "sessionStart",
		Detail: "Confidential feature Alpha",
		Prompt: "Secret Alpha code",
	})

	// Alpha 客户端应当立即收到该事件
	evAlpha, err := readSSEEvent(readerStreamA, 3*time.Second)
	if err != nil {
		t.Fatalf("Alpha stream did not receive event: %v", err)
	}
	if !strings.Contains(evAlpha.Data, "sess-alpha-private") {
		t.Errorf("Alpha stream received wrong event: %s", evAlpha.Data)
	}

	// Beta 客户端绝对不应当收到该事件 (超时等待 500ms，确认无数据推给 Beta)
	evBeta, errBeta := readSSEEvent(readerStreamB, 500*time.Millisecond)
	if errBeta == nil && strings.Contains(evBeta.Data, "sess-alpha-private") {
		t.Fatalf("CRITICAL: tenant Beta received confidential event from tenant Alpha! Data: %s", evBeta.Data)
	}

	// 3.4 跨租户篡改与越权防护：
	// Tenant Beta 试图发送中间执行事件篡改属于 Tenant Alpha 的私有会话 sess-alpha-private -> 返回 403 FORBIDDEN
	codeTamper, tamperResp := postHookEvent(t, inst.URL, "key-beta", task.EventPayload{
		ID:     "sess-alpha-private",
		Agent:  "Cursor",
		Event:  "beforeShellExecution",
		Detail: "malicious bash injection",
	})
	if codeTamper != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-tenant tampering, got %d (resp: %v)", codeTamper, tamperResp)
	}
	if extractErrorCode(tamperResp) != "FORBIDDEN" {
		t.Errorf("expected typed error code FORBIDDEN, got %v", tamperResp)
	}

	// Tenant Beta 试图删除属于 Tenant Alpha 的私有任务 -> 拒绝删除，Alpha 任务安全留存
	reqDelCross, _ := http.NewRequest(http.MethodDelete, inst.URL+"/api/tasks/sess-alpha-private", nil)
	reqDelCross.Header.Set("X-API-Key", "key-beta")
	respDelCross, err := client.Do(reqDelCross)
	if err != nil {
		t.Fatalf("failed to execute cross-tenant delete: %v", err)
	}
	defer respDelCross.Body.Close()
	var delResult map[string]interface{}
	_ = json.NewDecoder(respDelCross.Body).Decode(&delResult)
	if delResult["deleted"] == true {
		t.Fatalf("CRITICAL: cross-tenant deletion succeeded unexpectedly: %v", delResult)
	}

	// 校验 Alpha 任务依然完好存活
	codeVerify, tasksVerify := fetchTasks(t, inst.URL, "key-alpha")
	if codeVerify != http.StatusOK {
		t.Fatalf("failed to query tasks for Alpha: %d", codeVerify)
	}
	foundAlphaPrivate := false
	for _, t := range tasksVerify {
		if t.ID == "sess-alpha-private" {
			foundAlphaPrivate = true
			break
		}
	}
	if !foundAlphaPrivate {
		t.Fatalf("expected sess-alpha-private to survive cross-tenant delete attempt")
	}
}

// -----------------------------------------------------------------------------
// 4. Safe Inversion of Control: Steer, Abort, and Kill Host Safety
// -----------------------------------------------------------------------------

func TestE2E_Safe_InversionOfControl(t *testing.T) {
	inst := startTestServer(t, testServerOptions{})
	defer inst.Cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	// 4.1 动态 Steer 注入上下文并在下一次 Hook 拦截点无缝交付
	sessionSteer := "sess-ioc-steer-01"
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     sessionSteer,
		Agent:  "ZCode",
		Event:  "sessionStart",
		Detail: "Starting session for steer test",
		Prompt: "Initial user instruction",
	})

	// 调用 POST /api/tasks/{id}/steer 注入指导建议
	steerBody := `{"context":"Attention: Please strictly check race conditions and run go test -race","target_child_id":""}`
	reqSteer, _ := http.NewRequest(http.MethodPost, inst.URL+"/api/tasks/"+sessionSteer+"/steer", strings.NewReader(steerBody))
	reqSteer.Header.Set("Content-Type", "application/json")
	respSteer, err := client.Do(reqSteer)
	if err != nil {
		t.Fatalf("POST /api/tasks/{id}/steer failed: %v", err)
	}
	defer respSteer.Body.Close()
	if respSteer.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for steer injection, got %d", respSteer.StatusCode)
	}

	// 随后 Agent 执行工具动作 (beforeShellExecution) -> 响应必须携带刚刚注入的上下文指导
	codeHookSteer, respHookSteer := postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     sessionSteer,
		Agent:  "ZCode",
		Event:  "beforeShellExecution",
		Detail: "go test ./...",
	})
	if codeHookSteer != http.StatusOK {
		t.Fatalf("expected 200 for hook event after steer, got %d", codeHookSteer)
	}
	if respHookSteer["action"] != "allow" {
		t.Errorf("expected action allow, got %v", respHookSteer["action"])
	}
	// 验证 additional_context 或 agent_message 包含注入的文本
	addCtx, _ := respHookSteer["additional_context"].(string)
	agentMsg, _ := respHookSteer["agent_message"].(string)
	combinedMsg := addCtx + " " + agentMsg
	if !strings.Contains(combinedMsg, "strictly check race conditions") {
		t.Errorf("steer context was not delivered to Agent Hook response: %v", respHookSteer)
	}

	// 4.2 控制反转：Abort 端点软中断会话 -> 拦截后续 Pre-Action 工具执行
	sessionAbort := "sess-ioc-abort-01"
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     sessionAbort,
		Agent:  "Cursor",
		Event:  "sessionStart",
		Detail: "Session running destructive action",
		Prompt: "Refactoring database schemas",
	})

	// 调用 POST /api/tasks/{id}/abort 请求中断会话
	abortBody := `{"reason":"User manual emergency stop from dashboard"}`
	reqAbort, _ := http.NewRequest(http.MethodPost, inst.URL+"/api/tasks/"+sessionAbort+"/abort", strings.NewReader(abortBody))
	reqAbort.Header.Set("Content-Type", "application/json")
	respAbort, err := client.Do(reqAbort)
	if err != nil {
		t.Fatalf("POST /api/tasks/{id}/abort failed: %v", err)
	}
	defer respAbort.Body.Close()
	if respAbort.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for abort request, got %d", respAbort.StatusCode)
	}
	var abortData map[string]interface{}
	_ = json.NewDecoder(respAbort.Body).Decode(&abortData)
	if abortData["control_state"] != "abort_requested" {
		t.Errorf("expected control_state abort_requested, got %v", abortData["control_state"])
	}

	// 随后 Agent 试图执行工具 (beforeShellExecution) -> 响应必须返回 deny 并附带中断原因
	codeHookAbort, respHookAbort := postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:     sessionAbort,
		Agent:  "Cursor",
		Event:  "beforeShellExecution",
		Detail: "rm -rf /tmp/test-schema",
	})
	if codeHookAbort != http.StatusOK {
		t.Fatalf("expected 200 for hook event after abort, got %d", codeHookAbort)
	}
	if respHookAbort["action"] != "deny" {
		t.Fatalf("expected hook action deny after abort, got %v", respHookAbort["action"])
	}
	reason, _ := respHookAbort["reason"].(string)
	if !strings.Contains(reason, "User manual emergency stop") {
		t.Errorf("expected abort reason in hook response, got %s", reason)
	}

	// 4.3 强杀安全机制 (Kill Safety Check): 拒绝向 HostID/BootID 不匹配的异地任务发送本机 Kill
	sessionRemote := "sess-ioc-kill-remote"
	postHookEvent(t, inst.URL, "", task.EventPayload{
		ID:      sessionRemote,
		Agent:   "ZCode",
		Event:   "sessionStart",
		Detail:  "Remote session from another host",
		Prompt:  "Cloud runner task",
			HostID:  "remote-cloud-runner-node-99",
			BootID:  "boot-guid-11223344",
			PID:     99999,
			PGID:    99999,
	})

	reqKill, _ := http.NewRequest(http.MethodPost, inst.URL+"/api/tasks/"+sessionRemote+"/kill", nil)
	respKill, err := client.Do(reqKill)
	if err != nil {
		t.Fatalf("POST /api/tasks/{id}/kill failed: %v", err)
	}
	defer respKill.Body.Close()

	if respKill.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for mismatched host kill, got %d", respKill.StatusCode)
	}
	var killErr map[string]interface{}
	_ = json.NewDecoder(respKill.Body).Decode(&killErr)
	if extractErrorCode(killErr) != "HOST_MISMATCH" {
		t.Errorf("expected typed error HOST_MISMATCH, got %v", killErr)
	}
}

// -----------------------------------------------------------------------------
// 5. Batch Operations & Clean Persistence Drain
// -----------------------------------------------------------------------------

func TestE2E_BatchOperations_And_PersistenceDrain(t *testing.T) {
	tempDataDir, err := os.MkdirTemp("", "agent-monitor-e2e-persistence-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDataDir)

	// 阶段 1：启动服务器，录入任务并执行批量删除与优雅停机排空
	func() {
		inst := startTestServer(t, testServerOptions{customDir: tempDataDir})
		defer inst.Cleanup()

		client := &http.Client{Timeout: 5 * time.Second}

		// 创建 3 个任务
		taskIDs := []string{"task-batch-alpha", "task-batch-beta", "task-batch-gamma"}
		for _, tid := range taskIDs {
			postHookEvent(t, inst.URL, "", task.EventPayload{
				ID:     tid,
				Agent:  "ZCode",
				Event:  "sessionStart",
				Detail: "Batch test " + tid,
				Prompt: "Prompt for " + tid,
			})
		}

		// 验证 3 个任务均已生效
		codeAll, tasksAll := fetchTasks(t, inst.URL, "")
		if codeAll != http.StatusOK || len(tasksAll) != 3 {
			t.Fatalf("expected 3 tasks created, got %d (code %d)", len(tasksAll), codeAll)
		}

		// 批量删除指定的 task-batch-alpha
		delBody := `{"ids":["task-batch-alpha"]}`
		reqDel, _ := http.NewRequest(http.MethodDelete, inst.URL+"/api/tasks", strings.NewReader(delBody))
		reqDel.Header.Set("Content-Type", "application/json")
		respDel, err := client.Do(reqDel)
		if err != nil {
			t.Fatalf("DELETE /api/tasks failed: %v", err)
		}
		defer respDel.Body.Close()
		if respDel.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for batch delete, got %d", respDel.StatusCode)
		}
		var delRes map[string]interface{}
		_ = json.NewDecoder(respDel.Body).Decode(&delRes)
		if count, ok := delRes["count"].(float64); !ok || count != 1 {
			t.Fatalf("expected delete count 1, got %v", delRes["count"])
		}

		// 校验当前仅剩 2 个任务
		_, remainingTasks := fetchTasks(t, inst.URL, "")
		if len(remainingTasks) != 2 {
			t.Fatalf("expected 2 remaining tasks, got %d", len(remainingTasks))
		}
		for _, r := range remainingTasks {
			if r.ID == "task-batch-alpha" {
				t.Fatalf("task-batch-alpha was not deleted")
			}
		}

		// inst.Cleanup() 在此处触发，执行 MonitorService.CloseWithContext 排空写管道并关闭磁盘仓储
	}()

	// 阶段 2：模拟服务重启，从相同目录重新载入仓储 -> 验证持久化排空质量与墓碑去重
	func() {
		inst2 := startTestServer(t, testServerOptions{customDir: tempDataDir})
		defer inst2.Cleanup()

		client := &http.Client{Timeout: 5 * time.Second}

		codeRestored, restoredTasks := fetchTasks(t, inst2.URL, "")
		if codeRestored != http.StatusOK {
			t.Fatalf("failed to query tasks after restart: %d", codeRestored)
		}

		// task-batch-alpha 被删除，绝不能复活；task-batch-beta 与 task-batch-gamma 必须完好恢复
		if len(restoredTasks) != 2 {
			t.Fatalf("expected exactly 2 tasks restored from persistence, got %d", len(restoredTasks))
		}
		idMap := make(map[string]bool)
		for _, r := range restoredTasks {
			idMap[r.ID] = true
		}
		if idMap["task-batch-alpha"] {
			t.Fatalf("CRITICAL: deleted task-batch-alpha was restored from disk (tombstone failure)")
		}
		if !idMap["task-batch-beta"] || !idMap["task-batch-gamma"] {
			t.Fatalf("expected task-batch-beta and gamma restored, got: %v", idMap)
		}

		// 清空全部任务 (all=true)
		reqClearAll, _ := http.NewRequest(http.MethodDelete, inst2.URL+"/api/tasks?all=true", nil)
		respClearAll, err := client.Do(reqClearAll)
		if err != nil {
			t.Fatalf("DELETE /api/tasks?all=true failed: %v", err)
		}
		defer respClearAll.Body.Close()
		if respClearAll.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for clear all, got %d", respClearAll.StatusCode)
		}

		_, tasksEmpty := fetchTasks(t, inst2.URL, "")
		if len(tasksEmpty) != 0 {
			t.Fatalf("expected 0 tasks after clear all, got %d", len(tasksEmpty))
		}
	}()
}

// -----------------------------------------------------------------------------
// 6. Embedded Shell Accessibility, Bilingual I18N, and Keyboard Navigation Integrity
// -----------------------------------------------------------------------------

func TestE2E_StaticShell_A11y_I18N_And_PWA_Integrity(t *testing.T) {
	repoRoot := findRepoRoot(t)
	htmlPath := filepath.Join(repoRoot, "static/index.html")
	htmlBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("failed to read static/index.html: %v", err)
	}
	html := string(htmlBytes)

	// 6.1 WCAG 2.1 AA 标准无障碍标记
	a11yAssertions := []struct {
		name    string
		pattern string
	}{
		{"A11y live polite region", `id="a11y-live-polite" class="sr-only" aria-live="polite"`},
		{"A11y live assertive region", `id="a11y-live-assertive" class="sr-only" aria-live="assertive"`},
		{"Drawer dialog role", `<aside id="drawer" role="dialog" aria-modal="true"`},
		{"Confirm modal role", `<div id="confirm-modal"`},
		{"High-contrast focus visible ring", `:focus-visible`},
		{"Card focus-visible styling", `.task-card:focus-visible`},
		{"Reduced motion media preference", `prefers-reduced-motion`},
	}
	for _, a := range a11yAssertions {
		if !strings.Contains(html, a.pattern) {
			t.Errorf("static/index.html missing accessibility requirement [%s]: %q", a.name, a.pattern)
		}
	}

	// 6.2 双语国际化 (I18N) 对齐与离线存储定义
	i18nAssertions := []struct {
		name    string
		pattern string
	}{
		{"I18N zh dictionary", `zh: {`},
		{"I18N en dictionary", `en: {`},
		{"IndexedDB offline DB name", `const OFFLINE_DB_NAME = 'agent_monitor_offline_db'`},
		{"Offline mode banner", `id="offline-banner"`},
		{"Offline storage title label", `id="lbl-offline-storage-title"`},
		{"Clear offline cache button", `id="btn-clear-offline-cache"`},
	}
	for _, i18n := range i18nAssertions {
		if !strings.Contains(html, i18n.pattern) {
			t.Errorf("static/index.html missing I18N/PWA requirement [%s]: %q", i18n.name, i18n.pattern)
		}
	}

	// 6.3 键盘导航与 Focus Trap 支持
	keyboardAssertions := []struct {
		name    string
		pattern string
	}{
		{"Keydown event listener", `addEventListener('keydown'`},
		{"Focus trap logic", `trapFocus`},
		{"Escape key handling", `'Escape'`},
	}
	for _, kb := range keyboardAssertions {
		if !strings.Contains(html, kb.pattern) {
			t.Errorf("static/index.html missing keyboard navigation requirement [%s]: %q", kb.name, kb.pattern)
		}
	}
}
