package domain

// Language describes how to build and run submissions. The ids are the ones stored in
// solution.language and must not change.
type Language struct {
	Name   string
	Source string
	// Compile is run inside the sandbox; empty when there is nothing to build.
	Compile []string
	Run     []string
	// Grader says how to judge a problem solved by writing a function; nil when the language cannot.
	Grader *GraderBuild
	// Interpreted and JVM languages get the bonus configured in judge.json.
	Bonus bool
	// Threads and child processes the program may create.
	Processes int
}

// GraderBuild joins the submission with the grader of the problem, the file that owns the entry
// point: it reads the test, calls the function the submission wrote and prints the result.
type GraderBuild struct {
	// File is the name of the grader in the folder of the problem.
	File    string
	Compile []string
	// Run replaces the run command of the language when the grader is what starts; empty keeps it.
	Run []string
}

// JavaOptions are filled from judge.json (javaXms, javaXmx).
type JavaOptions struct {
	Xms string
	Xmx string
}

// LanguageByID returns the toolchain of a language id, or false when this judge does not support it.
func LanguageByID(id int, java JavaOptions) (Language, bool) {
	cpp := []string{"/usr/bin/g++", "Main.cc", "-o", "Main", "-fno-asm", "-Wall", "-lm", "--static", "-DONLINE_JUDGE"}
	python := Language{
		Name:    "Python 3",
		Source:  "Main.py",
		Compile: []string{"/usr/bin/python3", "-m", "py_compile", "Main.py"},
		Run:     []string{"/usr/bin/python3", "Main.py"},
		// grader.py imports the function: "from Main import solve".
		Grader: &GraderBuild{
			File:    "grader.py",
			Compile: []string{"/usr/bin/python3", "-m", "py_compile", "Main.py", "grader.py"},
			Run:     []string{"/usr/bin/python3", "grader.py"},
		},
		Bonus:     true,
		Processes: 1,
	}
	// grader.cpp has main() and is linked with the submission.
	grader := &GraderBuild{
		File:    "grader.cpp",
		Compile: []string{"/usr/bin/g++", "Main.cc", "grader.cpp", "-o", "Main", "-fno-asm", "-Wall", "-lm", "--static", "-std=c++17", "-DONLINE_JUDGE"},
	}
	javac := []string{"/usr/bin/javac", "-J" + java.Xms, "-J" + java.Xmx, "-encoding", "UTF-8", "Main.java"}
	// One collector thread keeps the JVM inside the process limit and its timing stable.
	jvm := []string{"/usr/bin/java", java.Xms, java.Xmx, "-XX:+UseSerialGC", "-Xss64m"}
	switch id {
	case 0:
		return Language{
			Name:      "C",
			Source:    "Main.c",
			Compile:   []string{"/usr/bin/gcc", "Main.c", "-o", "Main", "-fno-asm", "-Wall", "-lm", "--static", "-std=c11", "-DONLINE_JUDGE"},
			Run:       []string{"./Main"},
			Processes: 1,
		}, true
	case 1:
		return Language{Name: "C++", Source: "Main.cc", Compile: cpp, Grader: grader, Run: []string{"./Main"}, Processes: 1}, true
	case 16:
		// The id keeps its old name in the database; the learning paths teach C++17 (gcd, auto [a, b]).
		return Language{Name: "C++17", Source: "Main.cc", Compile: append(cpp, "-std=c++17"), Grader: grader, Run: []string{"./Main"}, Processes: 1}, true
	case 3:
		return Language{
			Name:    "Java",
			Source:  "Main.java",
			Compile: javac,
			Run:     append(jvm[:len(jvm):len(jvm)], "Main"),
			// grader.java holds the class "grader" with main(), which calls the static method of Main.
			Grader: &GraderBuild{
				File:    "grader.java",
				Compile: append(javac[:len(javac):len(javac)], "grader.java"),
				Run:     append(jvm[:len(jvm):len(jvm)], "grader"),
			},
			Bonus:     true,
			Processes: 64,
		}, true
	case 6, 17, 19:
		return python, true
	case 18:
		// The build cache stays in /tmp so it is not copied back with the binary.
		build := "GOCACHE=/tmp/go-build GOTOOLCHAIN=local CGO_ENABLED=0 /usr/bin/go build -o Main Main.go"
		return Language{Name: "Go", Source: "Main.go", Compile: []string{"/bin/sh", "-c", build}, Run: []string{"./Main"}, Processes: 64}, true
	case 21:
		return Language{
			Name:      "JavaScript",
			Source:    "Main.js",
			Compile:   []string{"/usr/local/bin/node", "--check", "Main.js"},
			Run:       []string{"/usr/local/bin/node", "Main.js"},
			Bonus:     true,
			Processes: 64,
		}, true
	case 20:
		// The interpreter runs the pseudocode as PSeInt itself does (functions, real division,
		// dynamic arrays). It always exits 0 and prints its errors on stdout, so:
		// - compiling is a syntax check that fails when pseint prints anything;
		// - it reads one value per line, so every token of the input goes on its own line;
		// - it prints "> " before each value it reads: removed at the start of each line;
		// - "Lin 4 (inst 1): ERROR 296: Division por cero" becomes a runtime error.
		// ponytail: a program that prints "> " at the start of a line, or reads after
		// "Escribir Sin Saltar", loses those characters and can get WA.
		check := `/usr/bin/dos2unix -q Main.psc; out=$(/usr/bin/pseint Main.psc --nouser --norun); [ -z "$out" ] || { printf '%s\n' "$out"; exit 1; }`
		run := `/usr/bin/tr -s ' \t\r\n' '\n' | /usr/bin/pseint Main.psc --nouser --fortest |` +
			` /usr/bin/awk '/^(> )*Lin [0-9]+ \(inst [0-9]+\): ERROR/ { sub(/^(> )+/, ""); print > "/dev/stderr"; failed = 1; next }` +
			` { sub(/^(> )+/, ""); print } END { exit failed }'`
		return Language{
			Name:      "PSeInt",
			Source:    "Main.psc",
			Compile:   []string{"/bin/sh", "-c", check},
			Run:       []string{"/bin/sh", "-c", run},
			Bonus:     true,
			Processes: 8,
		}, true
	}
	return Language{}, false
}

// SupportedLanguageIDs lists every id LanguageByID knows.
func SupportedLanguageIDs() []int {
	return []int{0, 1, 3, 6, 16, 17, 18, 19, 20, 21}
}
