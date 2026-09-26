package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/samuelloza/isolate-wrapper/src/application/services"
	"github.com/samuelloza/isolate-wrapper/src/domain"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/rabbitmq"
	"github.com/samuelloza/isolate-wrapper/src/infrastructure/testcaseprovider"
)

func main() {
	log.Println("Starting isolate-wrapper listener...")

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables.")
	}

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@127.0.0.1:5672/"
		log.Println("Using default RABBITMQ_URL:", rabbitURL)
	}

	testCasesPath := os.Getenv("TESTCASES_PATH")
	if testCasesPath == "" {
		testCasesPath = "/home/sam/project/github/isolate-wrapper/test/testcases"
		log.Println("Using default TESTCASES_PATH:", testCasesPath)
	}

	boxPool := services.NewBoxPool(10)

	rmq, err := rabbitmq.NewRabbitService(rabbitURL)
	if err != nil {
		log.Fatalf("RabbitMQ connection error: %v", err)
	}
	defer rmq.Close()

	fileSystemProvider := testcaseprovider.NewFileSystemTestCaseProvider(testCasesPath)

	handler := func(msg domain.EvaluationInput) (domain.EvaluationResult, error) {
		log.Printf("Received submission ID: %s | Language: %s | Problem: %d",
			msg.ID, msg.Language, msg.ProblemID)

		processor := services.RequestProcessor{
			Input:            msg,
			BoxPool:          boxPool,
			TestCaseProvider: fileSystemProvider,
		}

		result, err := processor.ProcessRequest()
		if err != nil {
			log.Printf("Error processing submission %s: %v", msg.ID, err)
			return result, err
		}

		log.Printf("Successfully processed submission %s | Passed: %d/%d",
			result.SubmitID, result.TotalPassed, result.TotalCases)
		return result, nil
	}

	if err := rmq.Listen("submission_queue", handler); err != nil {
		log.Fatalf("Listener error: %v", err)
	}
}
