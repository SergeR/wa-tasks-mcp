# wa-tasks-mcp

Локальный MCP-сервер для Webasyst Tasks. Запускается один раз, обслуживает несколько клиентов (Claude Code, Cursor) по Streamable HTTP.

## Что умеет

- `list_tasks` — получить список задач (inbox, по проекту, по статусу, поиск)
- `create_task` — создать задачу в проекте
- `task_action` — закрыть / перевести по workflow / вернуть
- `list_projects` — список проектов (нужен для create_task)
- `list_statuses` — список статусов

## Сборка

```powershell
cd wa-tasks-mcp
go mod tidy
go build -o wa-tasks-mcp.exe ./cmd/server
```

## Конфигурация

Через переменные окружения:

| Переменная | Описание | Пример |
|---|---|---|
| `TRACKER_API_BASE` | Базовый URL API Webasyst | `https://tracker.example.com/api.php` |
| `TRACKER_API_TOKEN` | `access_token` для Webasyst API | `abc123...` |
| `MCP_BEARER_SECRET` | Любая длинная случайная строка для защиты самого MCP-сервера | `openssl rand -hex 32` |
| `MCP_LISTEN_ADDR` | Адрес для прослушивания | `127.0.2.34:7777` |

Можно генератор секрета на Windows: `powershell -Command "[Convert]::ToBase64String((1..32 | %% { Get-Random -Max 256 }))"`

## Запуск вручную

Можно передать конфигурацию через переменные окружения напрямую:

```powershell
$env:TRACKER_API_BASE="https://tracker.example.com/api.php"
$env:TRACKER_API_TOKEN="ваш_токен"
$env:MCP_BEARER_SECRET="ваш_секрет"
$env:MCP_LISTEN_ADDR="127.0.2.34:7777"
.\wa-tasks-mcp.exe
```

Или через файл `.env` в той же директории, что и исполняемый файл:

```powershell
Copy-Item .env.example .env
# отредактируйте .env
.\wa-tasks-mcp.exe
```

Переменные из окружения имеют приоритет над `.env` — файл читается только для тех переменных, которые не заданы явно.

Проверка: `curl http://127.0.2.34:7777/healthz` → `ok`.

## Запуск как служба через NSSM

1. Скачать [NSSM](https://nssm.cc/download).
2. Установить службу:

```powershell
nssm install WaTasksMCP "C:\path\to\wa-tasks-mcp.exe"
nssm set WaTasksMCP AppEnvironmentExtra ^
  TRACKER_API_BASE=https://tracker.example.com/api.php ^
  TRACKER_API_TOKEN=ваш_токен ^
  MCP_BEARER_SECRET=ваш_секрет ^
  MCP_LISTEN_ADDR=127.0.2.34:7777
nssm set WaTasksMCP AppStdout C:\path\to\wa-tasks-mcp.log
nssm set WaTasksMCP AppStderr C:\path\to\wa-tasks-mcp.log
nssm start WaTasksMCP
```

Дальше управление через стандартный `services.msc` или `nssm start/stop/restart WaTasksMCP`.

## Подключение Claude Code

```bash
claude mcp add --transport http tracker http://tracker.local:7777/mcp \
  --header "Authorization: Bearer ваш_секрет"
```

Или вручную в `~/.claude.json` в секции `mcpServers`:

```json
{
  "mcpServers": {
    "tracker": {
      "type": "http",
      "url": "http://tracker.local:7777/mcp",
      "headers": {
        "Authorization": "Bearer ваш_секрет"
      }
    }
  }
}
```

(Если используете `tracker.local`, добавьте в `C:\Windows\System32\drivers\etc\hosts`: `127.0.2.34 tracker.local`.)

## Подключение Cursor

`~/.cursor/mcp.json` или `.cursor/mcp.json` в проекте:

```json
{
  "mcpServers": {
    "tracker": {
      "url": "http://tracker.local:7777/mcp",
      "headers": {
        "Authorization": "Bearer ваш_секрет"
      }
    }
  }
}
```

## Что ещё стоит добавить потом

- Кеш `list_projects` / `list_statuses` на 5–10 минут (эти данные меняются редко, а агенты будут дёргать их часто).
- Tool `update_task` поверх `tasks.tasks.update` — если понадобится менять не только статус, но и название/описание/исполнителя.
- MCP resources вместо tools для проектов и статусов — это более идиоматичный способ отдавать "справочные" данные клиенту.
- Структурированный output (`outputSchema` в tools) — Claude Code сможет лучше парсить ответы.
