package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the content of judge.json.
type Config struct {
	DBHost     string `json:"dbHost"`
	DBPort     int    `json:"dbPort"`
	DBUser     string `json:"dbUser"`
	DBPassword string `json:"dbPassword"`
	DBName     string `json:"dbName"`
	// Submissions judged at the same time, and seconds between two looks at the queue.
	Parallel     int `json:"parallel"`
	SleepSeconds int `json:"sleepSeconds"`
	// Language ids this judge may take; empty means every language it supports.
	Languages     []int  `json:"languages"`
	JavaXms       string `json:"javaXms"`
	JavaXmx       string `json:"javaXmx"`
	BonusTimeSec  int    `json:"bonusTimeSeconds"`
	BonusMemoryMB int    `json:"bonusMemoryMB"`
	// Similarity keeps accepted contest sources and serves the similarity requests of the admin.
	Similarity bool `json:"similarity"`
	// StuckMinutes queues again a submission claimed this long ago that never got a verdict.
	StuckMinutes int `json:"stuckMinutes"`
	// Shadow judges again what is already judged and writes to shadow_verdict only.
	Shadow bool `json:"shadow"`
}

// Load reads judge.json. An unknown key is an error, so a misspelled setting is not silently ignored.
func Load(path string) (Config, error) {
	config := Config{DBPort: 3306, Parallel: 1, SleepSeconds: 5, JavaXms: "-Xms64m", JavaXmx: "-Xmx128m", Similarity: true, StuckMinutes: 15}
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("%s: %w", path, err)
	}
	if config.DBHost == "" || config.DBUser == "" || config.DBName == "" {
		return config, fmt.Errorf("%s must define dbHost, dbUser and dbName", path)
	}
	config.Parallel = max(config.Parallel, 1)
	config.SleepSeconds = max(config.SleepSeconds, 1)
	return config, nil
}
