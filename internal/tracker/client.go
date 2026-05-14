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
	"time"
)

// Client — тонкая обёртка над Webasyst Tasks API.
// Все методы — RPC-style: /api.php/{method}.
type Client struct {
	baseURL     string // например, https://tracker.example.com/api.php
	accessToken string
	http        *http.Client
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
	ProjectID       int      `json:"project_id"`
	StatusID        int      `json:"status_id"`
	Name            string   `json:"name"`
	Text            string   `json:"text,omitempty"`
	AssignedContact *Contact `json:"assigned_contact,omitempty"`
	CreateContact   *Contact `json:"create_contact,omitempty"`
	CreateDatetime  string   `json:"create_datetime,omitempty"`
	UpdateDatetime  string   `json:"update_datetime,omitempty"`
	DueDate         string   `json:"due_date,omitempty"`
	Priority        int      `json:"priority,omitempty"`
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

type ActionInput struct {
	ID                int    `json:"id"`
	Action            string `json:"action"` // return | forward | close
	StatusID          int    `json:"status_id,omitempty"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
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
			return asList, nil
		}
	}

	// Попытка 2: объект с полем data ({"total_count":N,"data":[...]}).
	var wrap struct {
		Data []Task `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		return wrap.Data, nil
	}

	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}

func (c *Client) CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error) {
	var out Task
	if err := c.doPost(ctx, "tasks.tasks.add", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) TaskAction(ctx context.Context, in ActionInput) error {
	return c.doPost(ctx, "tasks.tasks.action", in, nil)
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var raw json.RawMessage
	if err := c.doGet(ctx, "tasks.projects.getList", nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 && raw[0] == '[' {
		var asList []Project
		if err := json.Unmarshal(raw, &asList); err == nil {
			return asList, nil
		}
	}
	var wrap struct {
		Data []Project `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		return wrap.Data, nil
	}
	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}

func (c *Client) ListStatuses(ctx context.Context) ([]Status, error) {
	var raw json.RawMessage
	if err := c.doGet(ctx, "tasks.statuses.getList", nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 && raw[0] == '[' {
		var asList []Status
		if err := json.Unmarshal(raw, &asList); err == nil {
			return asList, nil
		}
	}
	var wrap struct {
		Data []Status `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		return wrap.Data, nil
	}
	return nil, fmt.Errorf("unexpected response shape: %s", string(raw))
}
