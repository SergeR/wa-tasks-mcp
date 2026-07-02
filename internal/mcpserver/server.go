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
		Name: "update_task",
		Description: `Обновляет свойства задачи (название, описание, проект, срок, исполнителя, приоритет, статус и т.д.).
Идентификатор задачи: id (целое число) ИЛИ number (строка вида "57.11" из поля full_number). Одно из них обязательно.
Указывайте только те поля, которые нужно изменить — остальные свойства задачи останутся без изменений.
Для смены рабочего статуса задачи с переходом по workflow (закрыть/вернуть/переслать) используйте task_action, а не status_id здесь.`,
	}, updateTaskHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "task_action",
		Description: `Выполняет действие над задачей и переводит её в другой статус.
action: "close" — закрыть, "forward" — отправить дальше по workflow, "return" — вернуть.
Идентификатор задачи: id (целое число из поля id) ИЛИ number (строка вида "57.11" из поля full_number). Одно из них обязательно.
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

	mcp.AddTool(s, &mcp.Tool{
		Name: "add_comment",
		Description: `Добавляет комментарий к задаче.
Идентификатор задачи: task_id (целое число) ИЛИ number (строка вида "57.11" из поля full_number). Одно из них обязательно.
Возвращает запись лога с полем id — этот id нужен для update_comment.`,
	}, addCommentHandler(tc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_comment",
		Description: `Редактирует существующий комментарий к задаче.
id — ID записи в логе (возвращается из add_comment или берётся из поля comment_log_id задачи).
Заменяет текст комментария целиком.`,
	}, updateCommentHandler(tc))

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
	ID                int    `json:"id,omitempty"`
	Number            string `json:"number,omitempty"` // формат "57.11" — альтернатива id
	Action            string `json:"action"`            // return | forward | close
	StatusID          int    `json:"status_id,omitempty"`
	Text              string `json:"text,omitempty"`
	AssignedContactID int    `json:"assigned_contact_id,omitempty"`
}

func taskActionHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, taskActionArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args taskActionArgs) (*mcp.CallToolResult, any, error) {
		taskID := args.ID
		if taskID == 0 && args.Number != "" {
			tasks, err := tc.ListTasks(ctx, "number/"+args.Number, 1, 0)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving number %q: %w", args.Number, err)
			}
			if len(tasks) == 0 {
				return nil, nil, fmt.Errorf("task %q not found", args.Number)
			}
			taskID = tasks[0].ID
		}
		if taskID == 0 {
			return nil, nil, fmt.Errorf("id or number is required")
		}
		if args.Action != "return" && args.Action != "forward" && args.Action != "close" {
			return nil, nil, fmt.Errorf(`action must be one of: "return", "forward", "close"`)
		}
		err := tc.TaskAction(ctx, tracker.ActionInput{
			ID:                taskID,
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

type updateTaskArgs struct {
	ID                  int     `json:"id,omitempty"`
	Number              string  `json:"number,omitempty"` // формат "57.11" — альтернатива id
	Name                *string `json:"name,omitempty"`
	Text                *string `json:"text,omitempty"`
	AssignedContactID   *int    `json:"assigned_contact_id,omitempty"`
	ProjectID           *int    `json:"project_id,omitempty"`
	MilestoneID         *int    `json:"milestone_id,omitempty"`
	Priority            *int    `json:"priority,omitempty"`
	StatusID            *int    `json:"status_id,omitempty"`
	DueDate             *string `json:"due_date,omitempty"`
	AttachmentsToDelete []int   `json:"attachments_to_delete,omitempty"`
}

func updateTaskHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, updateTaskArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args updateTaskArgs) (*mcp.CallToolResult, any, error) {
		taskID := args.ID
		if taskID == 0 && args.Number != "" {
			tasks, err := tc.ListTasks(ctx, "number/"+args.Number, 1, 0)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving number %q: %w", args.Number, err)
			}
			if len(tasks) == 0 {
				return nil, nil, fmt.Errorf("task %q not found", args.Number)
			}
			taskID = tasks[0].ID
		}
		if taskID == 0 {
			return nil, nil, fmt.Errorf("id or number is required")
		}
		task, err := tc.UpdateTask(ctx, tracker.UpdateTaskInput{
			ID:                  taskID,
			Name:                args.Name,
			Text:                args.Text,
			AssignedContactID:   args.AssignedContactID,
			ProjectID:           args.ProjectID,
			MilestoneID:         args.MilestoneID,
			Priority:            args.Priority,
			StatusID:            args.StatusID,
			DueDate:             args.DueDate,
			AttachmentsToDelete: args.AttachmentsToDelete,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(task)
	}
}

type addCommentArgs struct {
	TaskID int    `json:"task_id,omitempty"`
	Number string `json:"number,omitempty"` // альтернатива task_id: "57.11"
	Text   string `json:"text"`
}

func addCommentHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, addCommentArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args addCommentArgs) (*mcp.CallToolResult, any, error) {
		taskID := args.TaskID
		if taskID == 0 && args.Number != "" {
			tasks, err := tc.ListTasks(ctx, "number/"+args.Number, 1, 0)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving number %q: %w", args.Number, err)
			}
			if len(tasks) == 0 {
				return nil, nil, fmt.Errorf("task %q not found", args.Number)
			}
			taskID = tasks[0].ID
		}
		if taskID == 0 {
			return nil, nil, fmt.Errorf("task_id or number is required")
		}
		if args.Text == "" {
			return nil, nil, fmt.Errorf("text is required")
		}
		entry, err := tc.AddComment(ctx, tracker.AddCommentInput{
			TaskID: taskID,
			Text:   args.Text,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(entry)
	}
}

type updateCommentArgs struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func updateCommentHandler(tc *tracker.Client) func(context.Context, *mcp.CallToolRequest, updateCommentArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args updateCommentArgs) (*mcp.CallToolResult, any, error) {
		if args.ID == 0 {
			return nil, nil, fmt.Errorf("id is required")
		}
		if args.Text == "" {
			return nil, nil, fmt.Errorf("text is required")
		}
		entry, err := tc.UpdateComment(ctx, tracker.UpdateCommentInput{
			ID:   args.ID,
			Text: args.Text,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(entry)
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
