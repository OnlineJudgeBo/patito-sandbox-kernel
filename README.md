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
| 21 | JavaScript | Node.js 22 |
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
  "bonusMemoryMB": 512,
  "similarity": true,
  "stuckMinutes": 15,
  "shadow": false
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
| `similarity` | keep accepted contest sources and serve similarity requests | true |
| `stuckMinutes` | queue again a submission claimed this long ago without a verdict; 0 turns it off | 15 |
| `shadow` | compare with the previous judge instead of judging (see below) | false |

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

## Runs from the IDE

A submission with a row in `custom_input` (or without a problem) is a run, not a submission: it
uses the inputs of `custom_input_case`, ends with result 13 and the program output in
`runtimeinfo`, and does not change any counter. An input with an expected output is compared.

## Similarity check

Accepted contest sources are kept in `data/contests/<contest>/problem/<problem>/<solution>.<ext>`.
Every accepted contest submission is compared right after its verdict is saved, with
[Dolos](https://dolos.ugent.be), against the accepted solutions other users sent to the same
problem in the same language. Its closest match goes to `similar_code` when it is 70% similar or more.

When the admin API leaves `data/contests/<contest>/similarity.request`, the whole contest is
checked again: the previous rows of that contest are deleted and every solution gets its closest match.

## Before going to production

Check that the host can run the sandbox (cgroup v2 and privileged containers):

```sh
docker run --rm --privileged ghcr.io/onlinejudgebo/patito-sandbox-kernel:develop --check
```

Compare with the judge in use. With `"shadow": true` nothing is judged for real: submissions that
already have a verdict are judged again, newest first, and both verdicts go to `shadow_verdict`.

```sql
SELECT legacy_result, result, COUNT(*) FROM shadow_verdict
WHERE result <> legacy_result GROUP BY legacy_result, result;
```

Give the judge its own database user:

```sql
CREATE USER 'judge'@'%' IDENTIFIED BY '...';
GRANT SELECT, UPDATE ON jol.solution TO 'judge'@'%';
GRANT SELECT, UPDATE ON jol.problem TO 'judge'@'%';
GRANT SELECT, UPDATE ON jol.user_activity TO 'judge'@'%';
GRANT SELECT ON jol.source_code TO 'judge'@'%';
GRANT SELECT ON jol.custom_input TO 'judge'@'%';
GRANT SELECT ON jol.custom_input_case TO 'judge'@'%';
GRANT SELECT, INSERT, DELETE ON jol.compileinfo TO 'judge'@'%';
GRANT SELECT, INSERT, DELETE ON jol.runtimeinfo TO 'judge'@'%';
GRANT SELECT, INSERT, DELETE ON jol.similar_code TO 'judge'@'%';
-- shadow mode only
GRANT CREATE ON jol.* TO 'judge'@'%';
GRANT SELECT, INSERT, UPDATE ON jol.shadow_verdict TO 'judge'@'%';
```

## Special judge and graders

- A problem with `spj` set (`1` or `Y`) is checked by `data/<problem_id>/spj`, the binary of the
  previous judge: it runs in a box as `spj <input> <answer> <output>` and exit code 0 means accepted.
- A problem with `data/<problem_id>/grader.cpp` is solved by writing a function. The grader and
  the `*.h` files next to it are compiled with the submission (C++ only) and the grader source is
  removed before the program runs.

## Score groups

A submission is judged on every test, instead of stopping at the first failure, when its problem
has `data/<problem_id>/scoring.json` or its contest is ranked by points (`contest.obi`).

```json
{ "groups": [
  { "name": "Subtarea 1", "points": 10, "type": "sum", "tests": "s1_*" },
  { "points": 20, "type": "min", "tests": ["s2_*", "extra"] }
] }
```

- `tests` are globs (`*`, `?`, `[set]`) over the test name without `.in`. A test may be in several
  groups; a test in none earns nothing.
- `type`: `sum` pays in proportion to the tests passed (default), `min` the worst test (all or
  nothing), `mul` the product. A whitespace-only difference still earns the test.
- Without groups the score is the share of accepted tests.
- The score goes to `solution.pass_rate` as a fraction from 0 to 1 and the points of each group to
  `solution_subtask`. The verdict shown is the highest code among the tests.
- Both need the group scoring migration of the admin API. Without it the judge keeps working, but
  a full score is stored as 0.99 and the groups are not recorded.

## Not supported yet

- Checkers given as source (testlib), and with them partial credit for a single test.
- The other languages of the previous judge (Pascal, C#, PHP, ...).
