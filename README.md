# PR Reviewer Assignment Service

Backend-сервис для автоматического назначения ревьюверов на pull request. Хранит команды и пользователей, выбирает до двух активных коллег автора и позволяет заменить ревьювера, пока PR не слит.

В репозитории находятся HTTP API на Go, PostgreSQL, OpenAPI-контракт и Docker Compose для локального запуска. Сервис хранит собственные PR; интеграция с GitHub или GitLab не требуется и не реализована.

## Что реализовано

- создание команды с добавлением или обновлением участников;
- получение состава команды и изменение активности пользователя;
- создание PR со случайным выбором 0–2 ревьюверов;
- замена конкретного ревьювера на активного участника его команды;
- идемпотентное слияние PR и запрет переназначения после `MERGED`;
- получение PR, на которые пользователь назначен ревьювером;
- валидация запросов и единый JSON-формат ошибок.

## Основные правила и решения

| Вопрос | Поведение |
|---|---|
| Кто может стать ревьювером? | Активный участник команды автора; сам автор исключается. |
| Что делать, если кандидатов мало? | Назначить одного или вернуть `assigned_reviewers: []`. PR всё равно создаётся. |
| Из какой команды выбирается замена? | Из текущей команды заменяемого ревьювера. Автор, прежний ревьювер и уже назначенные участники исключаются. |
| Что происходит при деактивации? | Пользователь перестаёт участвовать в новых назначениях. Существующие назначения сохраняются. |
| Можно ли повторить merge? | Да: возвращается `MERGED` с исходным временем слияния. |
| Можно ли повторно создать команду или PR? | Нет: `TEAM_EXISTS` или `PR_EXISTS`. |
| Что означает существующий `user_id` в новой команде? | Пользователь переносится в новую команду; имя и активность обновляются. Старые PR и назначения сохраняются. |
| Что возвращает `/users/getReview`? | Назначения на открытые и слитые PR. Для пользователя без назначений, в том числе неизвестного, возвращается пустой список. |

## Архитектура

```mermaid
flowchart LR
    Client[HTTP-клиент] --> Router[Chi router]
    Router --> Handlers[HTTP handlers]
    Handlers --> Services[Domain services]
    Services --> Repositories[PostgreSQL repositories]
    Repositories --> DB[(PostgreSQL)]
```

HTTP-слой разбирает запросы и сопоставляет ошибки с HTTP-статусами. Сервисы проверяют входные данные и правила назначения. Репозитории выполняют SQL и управляют транзакциями. Зависимости собираются в `cmd/server/main.go`.

Создание PR, merge и reassign выполняются целиком в транзакциях `REPEATABLE READ`. Merge и reassign блокируют строку PR, а выбор кандидатов удерживает блокировки пользователей до завершения назначения. Конфликты сериализации и deadlock повторяются до трёх попыток. Это позволяет согласовать изменение активности, переназначение и слияние между несколькими экземплярами API.

```text
cmd/server/                       запуск и завершение сервиса
internal/http/                    роутер, handlers, JSON-запросы и ответы
internal/domain/                  сервисы PR, команд и пользователей
internal/domain/dto/              модели запросов и ответов
internal/domain/interfaces/       интерфейсы хранилища
internal/infrastructure/postgres/ подключение, SQL, транзакции
pkg/validator/                    общие проверки входных данных
pkg/logger/                       настройка Zap
init.sql                          начальная схема PostgreSQL
openapi.yml                       контракт HTTP API
```

## Схема БД

```mermaid
erDiagram
    teams ||--o{ users : contains
    users ||--o{ pull_requests : authors
    pull_requests ||--o{ pr_reviewers : assignments
    users ||--o{ pr_reviewers : reviews
    teams {
        varchar team_name PK
        timestamp created_at
    }
    users {
        varchar user_id PK
        varchar username
        varchar team_name FK
        boolean is_active
    }
    pull_requests {
        varchar pull_request_id PK
        varchar pull_request_name
        varchar author_id FK
        varchar status
        timestamp created_at
        timestamp merged_at
    }
    pr_reviewers {
        integer id PK
        varchar pull_request_id FK
        varchar reviewer_id FK
        timestamp assigned_at
    }
```

Схема: [init.sql](init.sql). Внешние ключи связывают авторов и назначения с пользователями, а уникальная пара `(pull_request_id, reviewer_id)` запрещает повторное назначение одного человека на один PR. Создание команды с участниками и PR с назначениями выполняются атомарно.

Время создания и слияния PR приложение записывает в UTC в существующие столбцы `TIMESTAMP`. Исторические значения, записанные предыдущей версией в локальном часовом поясе, автоматически не пересчитываются: для этого нужен исходный часовой пояс.

## Запуск

Нужен Docker с Compose. Из корня репозитория:

```bash
make up
```

Команда автоматически выбирает `docker compose` или `docker-compose`, собирает образ и запускает API вместе с PostgreSQL. Без Make:

```bash
docker compose up --build -d
```

API доступен на `http://localhost:8080`. Схема применяется автоматически при первом создании volume. Начальных команд, пользователей и PR нет — их можно создать примером ниже.

Запуск работает без `.env`. Для собственных настроек скопируйте `.env.example` в `.env` и измените значения. Существующий `.env` имеет приоритет над значениями по умолчанию.

```bash
make logs       # логи контейнеров
make down       # остановка с сохранением данных
```

`make clean` удаляет также volume с данными. Изменения `init.sql` не применяются к уже созданной базе автоматически: это начальная схема, а не система миграций.

### Локально без контейнера API

Нужны Go 1.25+ и PostgreSQL 16. Можно запустить только базу из Compose:

```bash
docker compose up -d db
DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=pruser \
DB_PASSWORD=change_me DB_NAME=prservice go run ./cmd/server
```

Если изменили учётные данные или порт БД, подставьте свои значения. Go-приложение читает переменные окружения; файл `.env` автоматически загружает только Compose.

| Переменная | Назначение |
|---|---|
| `DB_HOST` | Адрес PostgreSQL при локальном запуске. В Compose всегда `db`. |
| `DB_PORT` | Порт БД, по умолчанию в Compose `5432` на хосте. Внутри сети контейнеров всегда `5432`. |
| `DB_USER`, `DB_PASSWORD`, `DB_NAME` | Учётные данные и база. Значения по умолчанию для Compose: `pruser`, `change_me`, `prservice`. |
| `PORT` | Порт HTTP, по умолчанию `8080`. В Compose меняет порт на хосте; внутри контейнера остаётся `8080`. |
| `ENVIRONMENT` | `production` включает JSON-логи; другие значения — логи для разработки. |

## Пример работы

Создать команду с автором и двумя потенциальными ревьюверами:

```bash
curl -sS http://localhost:8080/team/add \
  -H 'Content-Type: application/json' \
  -d '{"team_name":"backend","members":[
    {"user_id":"u1","username":"Alice","is_active":true},
    {"user_id":"u2","username":"Bob","is_active":true},
    {"user_id":"u3","username":"Carol","is_active":true}
  ]}'
```

Создать PR от имени `u1`. Ревьюверами станут `u2` и `u3`; порядок не гарантируется:

```bash
curl -sS http://localhost:8080/pullRequest/create \
  -H 'Content-Type: application/json' \
  -d '{"pull_request_id":"pr-1","pull_request_name":"Add search","author_id":"u1"}'

curl -sS 'http://localhost:8080/users/getReview?user_id=u2'
```

Попытка заменить `u2` вернёт `409 NO_CANDIDATE`: в этой команде больше нет подходящих участников.

```bash
curl -sS http://localhost:8080/pullRequest/reassign \
  -H 'Content-Type: application/json' \
  -d '{"pull_request_id":"pr-1","old_user_id":"u2"}'
```

Слить PR. Этот запрос можно повторять; последующие переназначения вернут `409 PR_MERGED`:

```bash
curl -sS http://localhost:8080/pullRequest/merge \
  -H 'Content-Type: application/json' \
  -d '{"pull_request_id":"pr-1"}'
```

## API

Полные схемы запросов и ответов: [openapi.yml](openapi.yml).

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/team/add` | Создать команду и создать/обновить участников. |
| GET | `/team/get?team_name=backend` | Получить состав команды. |
| POST | `/users/setIsActive` | Изменить активность пользователя. |
| GET | `/users/getReview?user_id=u2` | Получить назначения пользователя. |
| POST | `/pullRequest/create` | Создать PR и назначить ревьюверов. |
| POST | `/pullRequest/merge` | Слить PR. |
| POST | `/pullRequest/reassign` | Заменить ревьювера; поле запроса — `old_user_id`. |

Ошибки возвращаются в одном формате:

```json
{"error":{"code":"NO_CANDIDATE","message":"no active replacement candidate in team"}}
```

`400` означает неверные входные данные или `TEAM_EXISTS`, `404` — отсутствие нужной сущности, `409` — конфликт правил PR, `500` — внутреннюю ошибку.

## Проверки

```bash
make test
make test-race
make lint
```

Для `make lint` нужен установленный GolangCI-Lint v2. Команда проверяет код и форматирование, не изменяя исходники и не устанавливая инструменты автоматически.

Для интеграционных проверок репозиториев укажите отдельную тестовую PostgreSQL:

```bash
PR_SERVICE_TEST_DATABASE_URL='postgres://pruser:change_me@localhost:5432/prservice?sslmode=disable' \
  go test -race -count=1 -v ./internal/infrastructure/postgres
```

Каждый тест создаёт отдельную схему и удаляет её после выполнения. Пользователю тестовой БД нужно право создания схем.

HTTP E2E запускаются против работающего тестового API:

```bash
PR_SERVICE_E2E_URL=http://localhost:8080 make test-e2e
```

E2E создают команды, пользователей и PR с уникальными идентификаторами и оставляют эти данные в тестовой БД. Сценарии проверяют валидацию без изменения данных, конкурентное создание PR, одновременные merge и reassign, идемпотентность merge и перенос участников команды. Без соответствующих переменных интеграционные тесты и E2E пропускаются при обычном `make test`.

Исправления проходили отдельные циклы работы кодера и независимого ревьювера. [Результаты ревью и ограничения проверки](docs/review-summary.md).

## Границы проекта

- Авторизации и разделения доступа между командами нет: API рассчитан на доверенное окружение.
- GitHub/GitLab, webhook, уведомления и сам процесс проверки кода не интегрированы.
- Распределение случайное; текущая нагрузка ревьюверов не учитывается.
- Пагинация, удаление сущностей, аудит изменений и автоматическое переназначение при деактивации не реализованы.
- `init.sql` применяется только на пустом volume; для обновления существующей схемы нужна отдельная миграция.

## Стек

Go 1.25, Chi, pgx v5, PostgreSQL 16, Zap, OpenAPI 3.0, Docker Compose и GolangCI-Lint.
