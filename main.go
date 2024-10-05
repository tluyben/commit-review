package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/joho/godotenv"
)

//go:embed filesprompt.txt reviewprompt.txt
var embeddedFiles embed.FS

type Config struct {
	BaseURL    string
	Token      string
	LowLLM     string
	HighLLM    string
	Webhook    string
	System     string
	FilesPrompt  string
	ReviewPrompt string
}

func main() {
	config := loadConfig()

	// Get the last commit and its diff
	commitInfo := getLastCommitInfo()

	// Ask the LOW LLM for files to review
	filesToReview := getFilesToReview(config, commitInfo)

	// Read the files content
	fileContents := readFiles(filesToReview)

	// Ask the HIGH LLM for a critical review
	review := getCriticalReview(config, commitInfo, fileContents)

	// Send the result to the webhook
	sendWebhook(config.Webhook, review)
}

func loadConfig() Config {
	if envFile := os.Getenv("ENVFILE"); envFile != "" {
		godotenv.Load(envFile)
	}

	webhook := flag.String("webhook", "", "Webhook URL (mandatory)")
	system := flag.String("system", "", "System prompt")
	filesPrompt := flag.String("files-prompt", "", "Custom files prompt")
	reviewPrompt := flag.String("review-prompt", "", "Custom review prompt")

	flag.Parse()

	if *webhook == "" {
		fmt.Println("Error: -webhook parameter is mandatory")
		os.Exit(1)
	}

	config := Config{
		BaseURL:    getEnv("OR_BASE", ""),
		Token:      getEnv("OR_TOKEN", ""),
		LowLLM:     getEnv("OR_LOW", ""),
		HighLLM:    getEnv("OR_HIGH", ""),
		Webhook:    *webhook,
		System:     *system,
	}

	config.FilesPrompt = getPrompt("filesprompt.txt", *filesPrompt)
	config.ReviewPrompt = getPrompt("reviewprompt.txt", *reviewPrompt)

	return config
}

func getPrompt(embeddedFile, customPrompt string) string {
	if customPrompt != "" {
		content, err := ioutil.ReadFile(customPrompt)
		if err != nil {
			fmt.Printf("Error reading custom prompt file: %v\n", err)
			os.Exit(1)
		}
		return string(content)
	}

	content, err := embeddedFiles.ReadFile(embeddedFile)
	if err != nil {
		fmt.Printf("Error reading embedded file: %v\n", err)
		os.Exit(1)
	}
	return string(content)
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getLastCommitInfo() string {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%H")
	lastCommitHash, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting last commit hash:", err)
		os.Exit(1)
	}

	cmd = exec.Command("git", "diff", string(lastCommitHash)+"^!", "--", ".")
	diff, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit diff:", err)
		os.Exit(1)
	}

	cmd = exec.Command("git", "log", "-1", "--pretty=format:%B")
	commitMessage, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit message:", err)
		os.Exit(1)
	}

	return fmt.Sprintf("Commit: %s\n\nMessage: %s\n\nDiff:\n%s", lastCommitHash, commitMessage, diff)
}

func getFilesToReview(config Config, commitInfo string) []string {
	prompt := fmt.Sprintf(config.FilesPrompt, commitInfo)
	
	response := callLLM(config, config.LowLLM, prompt)
	
	// Remove backticks if present
	response = strings.Trim(response, "`")
	
	var files []string
	err := json.Unmarshal([]byte(response), &files)
	if err != nil {
		fmt.Println("Error parsing LLM response:", err)
		os.Exit(1)
	}
	
	return files
}

func readFiles(files []string) map[string]string {
	contents := make(map[string]string)
	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", file, err)
			continue
		}
		contents[file] = string(content)
	}
	return contents
}

func getCriticalReview(config Config, commitInfo string, fileContents map[string]string) string {
	fileContentStr := ""
	for file, content := range fileContents {
		fileContentStr += fmt.Sprintf("\n--- %s ---\n%s\n", file, content)
	}

	prompt := fmt.Sprintf(config.ReviewPrompt, commitInfo, fileContentStr)

	if config.System != "" {
		prompt = config.System + "\n\n" + prompt
	}

	return callLLM(config, config.HighLLM, prompt)
}

func callLLM(config Config, model string, prompt string) string {
	url := fmt.Sprintf("%s/v1/chat/completions", config.BaseURL)
	requestBody, _ := json.Marshal(map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.Token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error calling LLM:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	return result["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["content"].(string)
}

func sendWebhook(url string, content string) {
	requestBody, _ := json.Marshal(map[string]string{"content": content})
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		fmt.Println("Error sending webhook:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Println("Webhook sent successfully")
}