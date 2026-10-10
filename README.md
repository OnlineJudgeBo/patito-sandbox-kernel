# patito-sandbox-kernel

The judge of Patito Online Judge. It takes pending submissions from the judge database, compiles
and runs them inside [isolate](https://github.com/ioi/isolate), and writes the verdict back.

It is a drop-in replacement for `onlinejudge-kernel`: it reads the same `data/<problem_id>/`
folders and the same tables, so the web, the admin API and the IDE need no change.

## How it works

1. Every `sleepSeconds` seconds it looks for rows of `solution` with `result < 2`.
2. It claims a row with an atomic update, so several judges can share one database.
3. The source is compiled inside a sandbox box.
4. Each test runs in a fresh box with the limits of the problem. Judging stops at the first test
   that is not accepted (ICPC style).
5. The verdict, time, memory and the compiler or runtime message are saved, and the user and
   problem counters are refreshed.

Verdicts use the codes the rest of the platform already knows: 4 accepted, 5 presentation error,
6 wrong answer, 7 time limit, 8 memory limit, 9 output limit, 10 runtime error, 11 compilation error.

## Languages

| `solution.language` | Language | Toolchain in the image |
| --- | --- | --- |
| 0 | C | gcc |
| 1, 16 | C++ (16 adds `-std=c++11`) | g++ |
| 3 | Java | OpenJDK 17 |
| 6, 17, 19 | Python 3 | Python 3.12 |
| 18 | Go | Go 1.22 |
| 21 | JavaScript | Node.js 18 |
| 20 | PSeInt | `pseint` and `psexport` translate to C++, then g++ |

Java, Python and JavaScript get the extra time and memory of `bonusTimeSeconds` and `bonusMemoryMB`.
A submission in any other language is left pending.

## Configuration

`$CONF` (default `/home/judge/etc/judge.json`). Only the database keys are required; an unknown key is an error.

```json
{
  "dbHost": "patito-db",
  "dbPort": 3306,
  "dbUser": "judge",
  "dbPassword": "secret",
  "dbName": "jol",
  "parallel": 2,
  "sleepSeconds": 1,
  "languages": [0, 1, 3, 16, 18, 19, 20, 21],
  "javaXms": "-Xms64m",
  "javaXmx": "-Xmx128m",
  "bonusTimeSeconds": 2,
  "bonusMemoryMB": 512
}
```

| Key | Meaning | Default |
| --- | --- | --- |
| `dbHost`, `dbPort`, `dbUser`, `dbPassword`, `dbName` | database | port 3306 |
| `parallel` | submissions judged at the same time | 1 |
| `sleepSeconds` | seconds between two looks at the queue | 5 |
| `languages` | language ids this judge may take | every supported one |
| `javaXms`, `javaXmx` | JVM heap flags | `-Xms64m`, `-Xmx128m` |
| `bonusTimeSeconds`, `bonusMemoryMB` | extra allowance for Java, Python and JavaScript | 0 |

Tests are read from `$JUDGE_HOME/data/<problem_id>/` (default `/home/judge`): every `<name>.in`
with its `<name>.out`, in name order.

## Running

```bash
docker compose up --build
```

The container must be **privileged** and the host must use **cgroup v2**: isolate creates
namespaces and one control group per box. The entrypoint prepares the control groups itself,
no systemd is needed.

## Development

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.24 go test ./...
```

The unit tests replace isolate with a fake sandbox, so they run anywhere.

## Not supported yet

- Runs with a custom input from the IDE (rows with a `custom_input` entry are skipped).
- Special judges (checkers), score groups and graders.
- Code similarity checks.
- The other languages of the previous judge (Pascal, C#, PHP, ...).
