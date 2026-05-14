package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/you/wa-tasks-mcp/internal/tracker"
)

func New(tc *tracker.Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "wa-tasks-mcp",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_tasks",
		Description: `Возвращает список задач из таск-трекера.
Параметр filter использует hash-синтаксис Webasyst:
  - "inbox" — мои входящие
  - "outbox" — мои исходящие
  - "project/N" — задачи проекта N
  - "status/inprogress" или "status/done"
  - "assigned/N" — назначенные на пользователя N
  - "search/текст" — полнотекстовый поиск
  - "id/N" — одна задача по ID
  - "number/P.N" — задача по номеру вида "1.42"
Если filter пуст — возвращает inbox.`,
	}, listTasksHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_task",
		Description: "Создаёт новую задачу в указанном проекте. project_id обязателен — получить его можно через list_projects.",
	}, createTaskHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "task_action",
		Description: `Выполняет действие над задачей и переводит её в другой статус.
action: "close" — закрыть, "forward" — отправить дальше по workflow, "return" — вернуть.
status_id опционален — если не указан, используется статус по умолчанию для действия.
text — комментарий, который сохранится в логе.`,
	}, taskActionHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_projects",
		Description: "Возвращает доступные проекты с их ID и названиями. Нужен для создания задач и фильтрации.",
	}, listProjectsHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_statuses",
		Description: "Возвращает все статусы задач с их ID и названиями.",
	}, listStatusesHandler(tc))

	return s
}

// ----- handlers -----

type listTasksArgs struct {
	Filter string `json:"filter,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

func listTasksHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, listTasksArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args listTasksArgs) (*mcp.CallToolResult, any, error) {
		filter := args.Filter
		if filter == "" {
			filter = "inbox"
		}
		tasks, err := tc.ListTasks(ctx, filter, args.Limit, args.Offset)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(tasks)
	}
}

type createTaskArgs struct {
	ProjectID         int    `json:"project_id"`
	Name              string `json:"name"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
	StatusID          int    `json:"status_id,omitempty"`
	Priority          int    `json:"priority,omitempty"`
	DueDate           string `json:"due_date,omitempty"`
}

func createTaskHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, createTaskArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args createTaskArgs) (*mcp.CallToolResult, any, error) {
		if args.ProjectID == 0 {
			return nil, nil, fmt.Errorf("project_id is required")
		}
		if args.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		task, err := tc.CreateTask(ctx, tracker.CreateTaskInput{
			ProjectID:         args.ProjectID,
			Name:              args.Name,
			Text:              args.Text,
			AssignedContactID: args.AssignedContactID,
			StatusID:          args.StatusID,
			Priority:          args.Priority,
			DueDate:           args.DueDate,
			UUID:              uuid.NewString(),
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(task)
	}
}

type taskActionArgs struct {
	ID                int    `json:"id"`
	Action            string `json:"action"` // return | forward | close
	StatusID          int    `json:"status_id,omitempty"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
}

func taskActionHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, taskActionArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args taskActionArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == 0 {
			return nil, nil, fmt.Errorf("id is required")
		}
		if args.Action != "return" && args.Action != "forward" && args.Action != "close" {
			return nil, nil, fmt.Errorf(`action must be one of: "return", "forward", "close"`)
		}
		err := tc.TaskAction(ctx, tracker.ActionInput{
			ID:                args.ID,
			Action:            args.Action,
			StatusID:          args.StatusID,
			Text:              args.Text,
			AssignedContactID: args.AssignedContactID,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]string{"status": "ok"})
	}
}

type emptyArgs struct{}

func listProjectsHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, emptyArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
		projects, err := tc.ListProjects(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(projects)
	}
}

func listStatusesHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, emptyArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
		statuses, err := tc.ListStatuses(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(statuses)
	}
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(buf)},
		},
	}, nil, nil
}
