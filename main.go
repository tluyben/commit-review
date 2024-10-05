package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

//go:embed filesprompt.txt reviewprompt.txt
var embeddedFiles embed.FS

type Config struct {
	BaseURL      string
	Token        string
	LowLLM       string
	HighLLM      string
	Webhook      string
	System       string
	FilesPrompt  string
	ReviewPrompt string
}

func main() {
	config := loadConfig()

	// Get the commit info based on flags
	commitInfo := getCommitInfo(config)

	// Ask the LOW LLM for files to review
	filesToReview := getFilesToReview(config, commitInfo)

	// Read the files content
	fileContents := readFiles(filesToReview)

	// Ask the HIGH LLM for a critical review
	review := getCriticalReview(config, commitInfo, fileContents)

	// Add links to changed files
	review = addFileLinks(review, filesToReview)

	// Print the result to stdout
	fmt.Println(review)

	// Send the result to the webhook if available
	if config.Webhook != "" {
		sendWebhook(config.Webhook, review)
	}
}

func loadConfig() Config {
	// Load .env file if it exists
	godotenv.Load()

	webhook := flag.String("webhook", "", "Webhook URL (optional)")
	system := flag.String("system", "", "System prompt")
	filesPrompt := flag.String("files-prompt", "", "Custom files prompt")
	reviewPrompt := flag.String("review-prompt", "", "Custom review prompt")
	envFile := flag.String("env", "", "Path to custom .env file")
	_ = flag.String("review-hash", "", "Git hash to review (optional)")
	var reviewHashes multiStringFlag
	flag.Var(&reviewHashes, "review-hashes", "Two git hashes to review against each other (optional)")

	flag.Parse()

	// Load custom .env file if provided
	if *envFile != "" {
		godotenv.Load(*envFile)
	}

	config := Config{
		BaseURL:      getEnv("OR_BASE", ""),
		Token:        getEnv("OR_TOKEN", ""),
		LowLLM:       getEnv("OR_LOW", ""),
		HighLLM:      getEnv("OR_HIGH", ""),
		Webhook:      *webhook,
		System:       *system,
		FilesPrompt:  getPrompt("filesprompt.txt", *filesPrompt),
		ReviewPrompt: getPrompt("reviewprompt.txt", *reviewPrompt),
	}

	return config
}

type multiStringFlag []string

func (m *multiStringFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func getPrompt(embeddedFile, customPrompt string) string {
	if customPrompt != "" {
		content, err := os.ReadFile(customPrompt)
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

func getCommitInfo(config Config) string {
	var hash1, hash2 string
	reviewHash := flag.Lookup("review-hash").Value.String()
	reviewHashes := flag.Lookup("review-hashes").Value.(*multiStringFlag)

	if len(*reviewHashes) == 2 {
		hash1, hash2 = (*reviewHashes)[0], (*reviewHashes)[1]
	} else if reviewHash != "" {
		hash1 = reviewHash
		hash2 = getParentCommit(hash1)
	} else {
		hash1 = getLastCommitHash()
		hash2 = getParentCommit(hash1)
	}

	diff := getDiff(hash1, hash2)
	commitMessage := getCommitMessage(hash1)

	return fmt.Sprintf("Commit: %s\n\nMessage: %s\n\nDiff:\n%s", hash1, commitMessage, diff)
}

func getLastCommitHash() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting last commit hash:", err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(output))
}

func getParentCommit(hash string) string {
	cmd := exec.Command("git", "rev-parse", hash+"^")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting parent commit:", err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(output))
}

func getDiff(hash1, hash2 string) string {
	cmd := exec.Command("git", "diff", hash2, hash1, "--", ".")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit diff:", err)
		os.Exit(1)
	}
	return string(output)
}

func getCommitMessage(hash string) string {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%B", hash)
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit message:", err)
		os.Exit(1)
	}
	return string(output)
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

	// Filter out non-text files
	validFiles := []string{}
	for _, file := range files {
		if isTextFile(file) {
			validFiles = append(validFiles, file)
		}
	}

	return validFiles
}

func isTextFile(filename string) bool {
	extensions := []string{".txt", ".md", ".go", ".py", ".js", ".html", ".css", ".json", ".xml", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf"}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, validExt := range extensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

func readFiles(files []string) map[string]string {
	contents := make(map[string]string)
	for _, file := range files {
		content, err := os.ReadFile(file)
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
	_config := openai.DefaultConfig(config.Token)
	_config.BaseURL = config.BaseURL
	client := openai.NewClientWithConfig(_config)

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)

	if err != nil {
		fmt.Printf("ChatCompletion error: %v\n", err)
		return ""
	}

	return resp.Choices[0].Message.Content
}

func sendWebhook(url string, content string) {
	resp, err := http.Post(url, "text/plain", bytes.NewBufferString(content))
	if err != nil {
		fmt.Println("Error sending webhook:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Webhook sent successfully")
}

func addFileLinks(review string, files []string) string {
	linksSection := "\n\nChanged Files:\n"

	// Get the git remote URL
	gitConfig, err := exec.Command("git", "config", "--get", "remote.origin.url").Output()
	if err != nil {
		fmt.Println("Error getting git remote URL:", err)
		return review
	}

	// Get the current branch name
	branchBytes, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		fmt.Println("Error getting current branch:", err)
		return review
	}
	currentBranch := strings.TrimSpace(string(branchBytes))

	gitURL := strings.TrimSpace(string(gitConfig))
	gitURL = strings.TrimSuffix(gitURL, ".git")

	var baseURL string
	if strings.HasPrefix(gitURL, "git@") {
		// SSH-style URL
		parts := strings.SplitN(gitURL, ":", 2)
		if len(parts) != 2 {
			fmt.Println("Invalid SSH-style Git URL")
			return review
		}
		domain := strings.TrimPrefix(parts[0], "git@")
		path := parts[1]
		baseURL = fmt.Sprintf("https://%s/%s", domain, path)
	} else if strings.HasPrefix(gitURL, "https://") {
		// HTTPS-style URL
		baseURL = gitURL
	} else {
		fmt.Println("Unsupported Git URL format")
		return review
	}

	for _, file := range files {
		fileURL := fmt.Sprintf("%s/blob/%s/%s", baseURL, currentBranch, file)
		linksSection += fmt.Sprintf("- [%s](%s)\n", file, fileURL)
	}

	if len(files) > 0 {
		return review + linksSection
	}
	return review
}
