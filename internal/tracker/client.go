package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

// Client — тонкая обёртка над Webasyst Tasks API.
// Все методы — RPC-style: /api.php/{method}.
type Client struct {
	baseURL     string // например, https://tracker.example.com/api.php
	accessToken string
	http        *http.Client

	mu             sync.Mutex
	statusCache    []Status
	statusCacheAt  time.Time
	projectCache   []Project
	projectCacheAt time.Time
}

func New(baseURL, accessToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		http:        &http.Client{Timeout: 30 * time.Second},
	}
}

// ----- DTO -----

type Contact struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Task struct {
	ID              int      `json:"id"`
	Number          int      `json:"number"`
	FullNumber      string   `json:"full_number,omitempty"`
	ProjectID       int      `json:"project_id"`
	MilestoneID     *int     `json:"milestone_id,omitempty"`
	StatusID        int      `json:"status_id"`
	Name            string   `json:"name"`
	Text            string   `json:"text,omitempty"`
	AssignedContact *Contact `json:"assigned_contact,omitempty"`
	CreateContact   *Contact `json:"create_contact,omitempty"`
	CreateDatetime  string   `json:"create_datetime,omitempty"`
	UpdateDatetime  string   `json:"update_datetime,omitempty"`
	DueDate         string   `json:"due_date,omitempty"`
	Priority        int      `json:"priority,omitempty"`
	HiddenTimestamp *int     `json:"hidden_timestamp,omitempty"`
}

func fillFullNumber(tasks []Task) {
	for i := range tasks {
		if tasks[i].ProjectID > 0 && tasks[i].Number > 0 {
			tasks[i].FullNumber = fmt.Sprintf("%d.%d", tasks[i].ProjectID, tasks[i].Number)
		}
	}
}

type Project struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Status struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CreateTaskInput struct {
	ProjectID         int    `json:"project_id"`
	Name              string `json:"name"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
	StatusID          int    `json:"status_id,omitempty"`
	Priority          int    `json:"priority,omitempty"`
	DueDate           string `json:"due_date,omitempty"`
	UUID              string `json:"uuid"`
}

// UpdateTaskInput описывает частичное обновление задачи. Указатели позволяют
// отличить «поле не задано» (nil — оставить прежнее значение) от «поле
// сброшено» (например, пустая строка) — это важно, поскольку сам API
// tasks.tasks.update требует передавать все свойства задачи разом,
// заменяя её целиком, а не только измененные поля.
type UpdateTaskInput struct {
	ID                  int
	Name                *string
	Text                *string
	AssignedContactID   *int
	ProjectID           *int
	MilestoneID         *int
	Priority            *int
	StatusID            *int
	HiddenTimestamp     *int
	DueDate             *string
	FilesHash           *string
	AttachmentsToDelete []int
}

type updateTaskBody struct {
	ID                  int     `json:"id"`
	Name                string  `json:"name"`
	Text                string  `json:"text"`
	AssignedContactID   *int    `json:"assigned_contact_id"`
	ProjectID           int     `json:"project_id"`
	MilestoneID         *int    `json:"milestone_id"`
	Priority            int     `json:"priority"`
	StatusID            int     `json:"status_id"`
	HiddenTimestamp     *int    `json:"hidden_timestamp"`
	DueDate             *string `json:"due_date"`
	FilesHash           *string `json:"files_hash"`
	AttachmentsToDelete []int   `json:"attachments_to_delete,omitempty"`
}

type ActionInput struct {
	ID                int    `json:"id"`
	Action            string `json:"action"` // return | forward | close
	StatusID          int    `json:"status_id,omitempty"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
}

type AddCommentInput struct {
	TaskID int    `json:"task_id"`
	Text   string `json:"text"`
}

type UpdateCommentInput struct {
	ID   int    `json:"id"` // ID записи в логе (возвращается из AddComment)
	Text string `json:"text"`
}

// LogEntry — запись в логе действий задачи (комментарий, смена статуса и т.п.)
type LogEntry struct {
	ID       int    `json:"id"`
	TaskID   int    `json:"task_id,omitempty"`
	Text     string `json:"text,omitempty"`
	Datetime string `json:"datetime,omitempty"`
}

// ----- transport -----

type apiError struct {
	Err     string `json:"error"`
	Message string `json:"error_message"`
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Err, e.Message)
	}
	return e.Err
}

func (c *Client) doGet(ctx context.Context, method string, query url.Values, out any) error {
	u := c.baseURL + "/" + method
	if query != nil && len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	return c.do(req, out)
}

func (c *Client) doPost(ctx context.Context, method string, body any, out any) error {
	u := c.baseURL + "/" + method

	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	return c.do(req, out)
}

// setAuth добавляет токен в Authorization-заголовок.
// Webasyst поддерживает оба способа (query ?access_token=... и Bearer-заголовок),
// причём query имеет приоритет. Заголовок надёжнее: токен не попадает в
// access-логи прокси/веб-сервера и не утекает через Referer.
func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Webasyst иногда отдаёт ошибку с HTTP 200 + полем error в теле.
	// Сначала пробуем распарсить как ошибку.
	var maybeErr apiError
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &maybeErr)
		if maybeErr.Err != "" {
			return &maybeErr
		}
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// ----- methods -----

func (c *Client) ListTasks(ctx context.Context, hash string, limit, offset int) ([]Task, error) {
	q := url.Values{}
	if hash != "" {
		q.Set("hash", hash)
	}
	q.Set("offset", strconv.Itoa(offset))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	// API возвращает объект { tasks: [...] } — точная форма зависит от
	// версии. Парсим гибко.
	var raw json.RawMessage
	if err := c.doGet(ctx, "tasks.tasks.getList", q, &raw); err != nil {
		return nil, err
	}

	// Попытка 1: массив задач прямо в корне.
	if len(raw) > 0 && raw[0] == '[' {
		var asList []Task
		if err := json.Unmarshal(raw, &asList); err == nil {
			fillFullNumber(asList)
			return asList, nil
		}
	}

	// Попытка 2: объект с полем data ({"total_count":N,"data":[...]}).
	var wrap struct {
		Data []Task `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		fillFullNumber(wrap.Data)
		return wrap.Data, nil
	}

	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}

func (c *Client) CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error) {
	var out Task
	if err := c.doPost(ctx, "tasks.tasks.add", in, &out); err != nil {
		return nil, err
	}
	if out.ProjectID > 0 && out.Number > 0 {
		out.FullNumber = fmt.Sprintf("%d.%d", out.ProjectID, out.Number)
	}
	return &out, nil
}

// UpdateTask обновляет задачу. Поскольку tasks.tasks.update заменяет свойства
// задачи целиком, метод сначала подгружает её текущее состояние и заполняет
// не указанные в in поля прежними значениями, чтобы их не потерять.
func (c *Client) UpdateTask(ctx context.Context, in UpdateTaskInput) (*Task, error) {
	if in.ID == 0 {
		return nil, fmt.Errorf("id is required")
	}

	tasks, err := c.ListTasks(ctx, fmt.Sprintf("id/%d", in.ID), 1, 0)
	if err != nil {
		return nil, fmt.Errorf("fetching current task: %w", err)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("task %d not found", in.ID)
	}
	cur := tasks[0]

	body := updateTaskBody{
		ID:                  in.ID,
		Name:                cur.Name,
		Text:                cur.Text,
		ProjectID:           cur.ProjectID,
		MilestoneID:         cur.MilestoneID,
		Priority:            cur.Priority,
		StatusID:            cur.StatusID,
		HiddenTimestamp:     cur.HiddenTimestamp,
		AttachmentsToDelete: in.AttachmentsToDelete,
	}
	if cur.AssignedContact != nil {
		id := cur.AssignedContact.ID
		body.AssignedContactID = &id
	}
	if cur.DueDate != "" {
		d := cur.DueDate
		body.DueDate = &d
	}

	if in.Name != nil {
		body.Name = *in.Name
	}
	if in.Text != nil {
		body.Text = *in.Text
	}
	if in.AssignedContactID != nil {
		body.AssignedContactID = in.AssignedContactID
	}
	if in.ProjectID != nil {
		body.ProjectID = *in.ProjectID
	}
	if in.MilestoneID != nil {
		body.MilestoneID = in.MilestoneID
	}
	if in.Priority != nil {
		body.Priority = *in.Priority
	}
	if in.StatusID != nil {
		body.StatusID = *in.StatusID
	}
	if in.HiddenTimestamp != nil {
		body.HiddenTimestamp = in.HiddenTimestamp
	}
	if in.DueDate != nil {
		body.DueDate = in.DueDate
	}
	if in.FilesHash != nil {
		body.FilesHash = in.FilesHash
	}

	var out Task
	if err := c.doPost(ctx, "tasks.tasks.update", body, &out); err != nil {
		return nil, err
	}
	if out.ProjectID > 0 && out.Number > 0 {
		out.FullNumber = fmt.Sprintf("%d.%d", out.ProjectID, out.Number)
	}
	return &out, nil
}

func (c *Client) TaskAction(ctx context.Context, in ActionInput) error {
	return c.doPost(ctx, "tasks.tasks.action", in, nil)
}

func (c *Client) AddComment(ctx context.Context, in AddCommentInput) (*LogEntry, error) {
	var out LogEntry
	if err := c.doPost(ctx, "tasks.comments.add", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateComment(ctx context.Context, in UpdateCommentInput) (*LogEntry, error) {
	var out LogEntry
	if err := c.doPost(ctx, "tasks.comments.update", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	c.mu.Lock()
	if c.projectCache != nil && time.Since(c.projectCacheAt) < cacheTTL {
		cached := c.projectCache
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	var raw json.RawMessage
	if err := c.doGet(ctx, "tasks.projects.getList", nil, &raw); err != nil {
		return nil, err
	}

	var result []Project
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &result); err == nil {
			c.mu.Lock()
			c.projectCache, c.projectCacheAt = result, time.Now()
			c.mu.Unlock()
			return result, nil
		}
	}
	var wrap struct {
		Data []Project `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		c.mu.Lock()
		c.projectCache, c.projectCacheAt = wrap.Data, time.Now()
		c.mu.Unlock()
		return wrap.Data, nil
	}
	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}

func (c *Client) ListStatuses(ctx context.Context) ([]Status, error) {
	c.mu.Lock()
	if c.statusCache != nil && time.Since(c.statusCacheAt) < cacheTTL {
		cached := c.statusCache
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	var raw json.RawMessage
	if err := c.doGet(ctx, "tasks.statuses.getList", nil, &raw); err != nil {
		return nil, err
	}

	var result []Status
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &result); err == nil {
			c.mu.Lock()
			c.statusCache, c.statusCacheAt = result, time.Now()
			c.mu.Unlock()
			return result, nil
		}
	}
	var wrap struct {
		Data []Status `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		c.mu.Lock()
		c.statusCache, c.statusCacheAt = wrap.Data, time.Now()
		c.mu.Unlock()
		return wrap.Data, nil
	}
	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}
