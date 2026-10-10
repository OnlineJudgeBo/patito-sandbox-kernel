package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is read from the same judge.conf the previous judge used, so this one is a drop-in replacement.
type Config struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	// Submissions judged at the same time, and seconds between two looks at the queue.
	Parallel     int
	SleepSeconds int
	// Language ids this judge may take; empty means every language it supports.
	Languages     []int
	JavaXms       string
	JavaXmx       string
	BonusTimeSec  int
	BonusMemoryMB int
}

// Load parses "KEY=value" lines. Unknown keys are ignored: judge.conf has settings of the previous judge.
func Load(path string) (Config, error) {
	config := Config{DBPort: 3306, Parallel: 1, SleepSeconds: 5, JavaXms: "-Xms64m", JavaXmx: "-Xmx128m"}
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		number, _ := strconv.Atoi(value)
		switch key {
		case "OJ_HOST_NAME":
			config.DBHost = value
		case "OJ_PORT_NUMBER":
			config.DBPort = number
		case "OJ_USER_NAME":
			config.DBUser = value
		case "OJ_PASSWORD":
			config.DBPassword = value
		case "OJ_DB_NAME":
			config.DBName = value
		case "OJ_RUNNING":
			config.Parallel = number
		case "OJ_SLEEP_TIME":
			config.SleepSeconds = number
		case "OJ_JAVA_TIME_BONUS":
			config.BonusTimeSec = number
		case "OJ_JAVA_MEMORY_BONUS":
			config.BonusMemoryMB = number
		case "OJ_JAVA_XMS":
			config.JavaXms = value
		case "OJ_JAVA_XMX":
			config.JavaXmx = value
		case "OJ_LANG_SET":
			for _, item := range strings.Split(value, ",") {
				if id, err := strconv.Atoi(strings.TrimSpace(item)); err == nil {
					config.Languages = append(config.Languages, id)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return config, err
	}
	if config.DBHost == "" || config.DBUser == "" || config.DBName == "" {
		return config, fmt.Errorf("%s must define OJ_HOST_NAME, OJ_USER_NAME and OJ_DB_NAME", path)
	}
	if config.Parallel < 1 {
		config.Parallel = 1
	}
	if config.SleepSeconds < 1 {
		config.SleepSeconds = 1
	}
	return config, nil
}
