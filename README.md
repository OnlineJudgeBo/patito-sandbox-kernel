# isolate-wrapper

**isolate-wrapper** is the core evaluation engine for the Patito Virtual Judge.  
It listens to a RabbitMQ queue, receives submitted code, safely runs it using `isolate`, and sends the result back via an async callback.

---

## Installing `isolate`

```bash
sudo apt install pkg-config libcap-dev asciidoc libsystemd-dev
git clone git@github.com:ioi/isolate.git
cd isolate
make
sudo make install

cd systemd
sudo cp * /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable isolate
sudo systemctl start isolate
```

---

## JSON Task Format

This is the expected format for sending a task to `isolate-wrapper`:

```json
{
  "id": "123456",
  "uniqId": "abc123",
  "boxId": 0,
  "problemId": 1000,
  "language": "cpp",
  "sourceCode": "#include <iostream>\nusing namespace std;\nint main() {\n    int a;\n    cin >> a;\n    cout << a * 10 << endl;\n    return 0;\n}",
  "runLimits": {
    "time": 2,
    "memory": 65536,
    "output": 1024
  },
  "testCases": []
}
```

---

## Testing with Docker Compose

### MySQL read-only tasks

SQL submissions use `language: "mysql"`. The contestant query stays in
`sourceCode`; trusted schemas, seed data and expected result sets are carried
in the `sql` contract. Only `SELECT` access is granted to a short-lived user
inside a dedicated, disposable database.

```json
{
  "id": "sql-123",
  "uniqId": "sql-123-attempt-1",
  "problemId": 2000,
  "language": "mysql",
  "sourceCode": "SELECT name FROM vectors WHERE dimension >= 3 ORDER BY name",
  "runLimits": { "time": 5, "memory": 65536, "output": 1024 },
  "sql": {
    "dialect": "mysql",
    "orderMatters": true,
    "maxRows": 1000,
    "testCases": [{
      "schema": "CREATE TABLE vectors (id INT PRIMARY KEY, name VARCHAR(50), dimension INT)",
      "seed": "INSERT INTO vectors VALUES (1, 'a', 2), (2, 'b', 3)",
      "expected": { "columns": ["name"], "rows": [["b"]] }
    }]
  }
}
```

Configure `MYSQL_SANDBOX_ADMIN_DSN` with a MySQL instance used **only** for
disposable evaluations. It must never point to the platform database. Compose
provides an internal-only MySQL 8.4 service without a published port.

The first version is read-only, accepts one statement, caps results at 10,000
rows and cases at 30 seconds, preserves `NULL`, and can compare with or without
row ordering.

For asynchronous processing, publish an envelope to `submission_queue` and
provide the durable queue that must receive the result:

```json
{
  "input": { "id": "sql-123", "language": "mysql", "sql": {} },
  "reply_queue": "evaluation_results"
}
```

The listener declares both queues, consumes one message at a time, publishes
the evaluation result persistently, and acknowledges the submission only after
the result has been published. Raw tasks without an envelope remain supported,
but do not produce an asynchronous reply.

```bash
go test ./...

MYSQL_SANDBOX_TEST_DSN='root:password@tcp(127.0.0.1:3306)/?timeout=5s' \
  go test ./src/infrastructure/mysqlrunner
```

To test the service:

1. Run:

```bash
docker compose run
```

2. Open [http://localhost:15672/](http://localhost:15672/)  
   Username: `guest`  
   Password: `guest`

3. Create a queue named:

```
submission_queue
```

---

## Architecture

![Architecture](image.png)
