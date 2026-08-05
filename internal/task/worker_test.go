package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Bin-hy/bin-rag/internal/config"
	"github.com/Bin-hy/bin-rag/internal/pipeline"
	"github.com/Bin-hy/bin-rag/internal/store"
)

// fakePipeline 实现 pipeline.Pipeline
type fakePipeline struct {
	err      error
	chunkIDs []string
}

func (f *fakePipeline) Ingest(ctx context.Context, req pipeline.IngestRequest) ([]string, error) {
	return f.chunkIDs, f.err
}

// fakeStore 内存实现 store.Store（worker 测试用）
type fakeStore struct {
	mu          sync.Mutex
	tasks       map[string]store.Task
	docs        map[string]store.Document
	resetCalled bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{tasks: make(map[string]store.Task), docs: make(map[string]store.Document)}
}

func (f *fakeStore) CreateKB(ctx context.Context, kb store.KnowledgeBase) error { return nil }
func (f *fakeStore) ListKBs(ctx context.Context) ([]store.KnowledgeBase, error) { return nil, nil }
func (f *fakeStore) GetKB(ctx context.Context, id string) (*store.KnowledgeBase, error) {
	return nil, nil
}
func (f *fakeStore) UpdateKB(ctx context.Context, kb store.KnowledgeBase) error   { return nil }
func (f *fakeStore) DeleteKB(ctx context.Context, id string) error                { return nil }
func (f *fakeStore) CreateDocument(ctx context.Context, doc store.Document) error { return nil }
func (f *fakeStore) ListDocuments(ctx context.Context, kbID string) ([]store.Document, error) {
	return nil, nil
}
func (f *fakeStore) GetDocument(ctx context.Context, id string) (*store.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[id]
	if !ok {
		return nil, errors.New("文档不存在")
	}
	cp := d
	return &cp, nil
}
func (f *fakeStore) UpdateDocumentStatus(ctx context.Context, id string, status string, chunkIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.docs[id]
	d.Status = status
	d.ChunkIDs = chunkIDs
	f.docs[id] = d
	return nil
}
func (f *fakeStore) DeleteDocument(ctx context.Context, id string) error { return nil }
func (f *fakeStore) CreateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) GetTask(ctx context.Context, id string) (*store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	cp := t
	return &cp, nil
}
func (f *fakeStore) ListTasks(ctx context.Context, kbID string) ([]store.Task, error) {
	return nil, nil
}
func (f *fakeStore) UpdateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[t.ID] = t
	return nil
}
func (f *fakeStore) ClaimPendingTasks(ctx context.Context, limit int) ([]store.Task, error) {
	return nil, nil
}
func (f *fakeStore) ResetProcessingTasks(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalled = true
	return nil
}
func (f *fakeStore) CreateAPIKey(ctx context.Context, k store.APIKey) error  { return nil }
func (f *fakeStore) ListAPIKeys(ctx context.Context) ([]store.APIKey, error) { return nil, nil }
func (f *fakeStore) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	return nil, nil
}
func (f *fakeStore) SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) error { return nil }
func (f *fakeStore) DeleteAPIKey(ctx context.Context, id string) error                   { return nil }
func (f *fakeStore) TouchAPIKey(ctx context.Context, id string) error                    { return nil }
func (f *fakeStore) HistoryStore() store.HistoryStore                                    { return nil }
func (f *fakeStore) Migrate(ctx context.Context) error                                   { return nil }
func (f *fakeStore) Close()                                                              {}

// 任务成功：completed + 文档状态回填 chunkIDs
func TestProcess_Success(t *testing.T) {
	fs := newFakeStore()
	fs.docs["doc-1"] = store.Document{ID: "doc-1", FilePath: "/tmp/nonexist.txt"}

	// 文件路径不存在会 os.Open 失败——用真实临时文件
	fs.docs["doc-1"] = store.Document{ID: "doc-1", FilePath: createTempFile(t)}

	w := &defaultWorkerPool{
		cfg:      config.ServerConfig{TaskMaxRetries: 3},
		store:    fs,
		pipeline: &fakePipeline{chunkIDs: []string{"c1", "c2"}},
	}

	w.process(context.Background(), store.Task{
		ID: "task-1", KBID: "kb-1", DocumentID: "doc-1", Status: store.TaskStatusProcessing,
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	task := fs.tasks["task-1"]
	if task.Status != store.TaskStatusCompleted {
		t.Errorf("任务应 completed，实际 %s", task.Status)
	}
	doc := fs.docs["doc-1"]
	if doc.Status != store.DocStatusCompleted || len(doc.ChunkIDs) != 2 {
		t.Errorf("文档状态未回填: %+v", doc)
	}
}

// 失败未超上限：回 pending 且重试次数 +1
func TestProcess_RetryNotExceeded(t *testing.T) {
	fs := newFakeStore()
	fs.docs["doc-1"] = store.Document{ID: "doc-1", FilePath: createTempFile(t)}

	w := &defaultWorkerPool{
		cfg:      config.ServerConfig{TaskMaxRetries: 3},
		store:    fs,
		pipeline: &fakePipeline{err: errors.New("ingest boom")},
	}

	w.process(context.Background(), store.Task{
		ID: "task-1", KBID: "kb-1", DocumentID: "doc-1", Status: store.TaskStatusProcessing, RetryCount: 0,
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	task := fs.tasks["task-1"]
	if task.Status != store.TaskStatusPending {
		t.Errorf("未超上限应回 pending，实际 %s", task.Status)
	}
	if task.RetryCount != 1 {
		t.Errorf("重试次数应 +1，实际 %d", task.RetryCount)
	}
	if task.ErrorMessage == "" {
		t.Error("应记录错误信息")
	}
}

// 失败超上限：failed + 错误信息
func TestProcess_RetryExceeded(t *testing.T) {
	fs := newFakeStore()
	fs.docs["doc-1"] = store.Document{ID: "doc-1", FilePath: createTempFile(t)}

	w := &defaultWorkerPool{
		cfg:      config.ServerConfig{TaskMaxRetries: 1},
		store:    fs,
		pipeline: &fakePipeline{err: errors.New("ingest boom")},
	}

	w.process(context.Background(), store.Task{
		ID: "task-1", KBID: "kb-1", DocumentID: "doc-1", Status: store.TaskStatusProcessing, RetryCount: 1,
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	task := fs.tasks["task-1"]
	if task.Status != store.TaskStatusFailed {
		t.Errorf("超上限应 failed，实际 %s", task.Status)
	}
	if !errors.Is(errors.New(task.ErrorMessage), errors.New("ingest boom")) {
		if task.ErrorMessage != "ingest boom" {
			t.Errorf("错误信息不符: %q", task.ErrorMessage)
		}
	}
}

// 文档不存在：未超上限走重试，超上限 failed
func TestProcess_DocMissing(t *testing.T) {
	fs := newFakeStore() // 无文档

	w := &defaultWorkerPool{
		cfg:      config.ServerConfig{TaskMaxRetries: 3},
		store:    fs,
		pipeline: &fakePipeline{},
	}

	// 已到重试上限 → failed
	w.process(context.Background(), store.Task{
		ID: "task-1", KBID: "kb-1", DocumentID: "missing", Status: store.TaskStatusProcessing, RetryCount: 3,
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.tasks["task-1"].Status != store.TaskStatusFailed {
		t.Errorf("文档缺失且已达上限应 failed，实际 %s", fs.tasks["task-1"].Status)
	}
}

// Start 时调用 ResetProcessingTasks（重启恢复）
func TestStart_CallsReset(t *testing.T) {
	fs := newFakeStore()
	w := NewWorkerPool(config.ServerConfig{WorkerCount: 1}, fs, &fakePipeline{})

	w.Start(context.Background())
	time.Sleep(50 * time.Millisecond) // 给 Start 一点执行时间
	w.Shutdown()

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if !fs.resetCalled {
		t.Error("Start 应调用 ResetProcessingTasks")
	}
}

// 并发处理多任务无竞争
func TestProcess_Concurrent(t *testing.T) {
	fs := newFakeStore()
	for i := 0; i < 20; i++ {
		docID := string(rune('a' + i))
		fs.docs[docID] = store.Document{ID: docID, FilePath: createTempFile(t)}
	}

	w := &defaultWorkerPool{
		cfg:      config.ServerConfig{TaskMaxRetries: 3},
		store:    fs,
		pipeline: &fakePipeline{chunkIDs: []string{"c1"}},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			docID := string(rune('a' + i))
			w.process(context.Background(), store.Task{
				ID: "task-" + docID, KBID: "kb-1", DocumentID: docID, Status: store.TaskStatusProcessing,
			})
		}(i)
	}
	wg.Wait()
}

// createTempFile 创建临时文件并返回路径
func createTempFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(f, []byte("测试内容"), 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	return f
}
