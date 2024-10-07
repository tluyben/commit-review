package main

import (
	"bytes"
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
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

type multiStringFlag []string

func main() {
	config := loadConfig()

	// Get the commit info based on flags
	commitInfo := getCommitInfo(config)

	// fmt.Println(commitInfo)
	// os.Exit(1)

	// Ask the LOW LLM for files to review
	filesToReview := getFilesToReview(config, commitInfo)

	// fmt.Println("files to review", filesToReview)
	// os.Exit(1)

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

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
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

func getCommitMessage(hash string) string {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%B", hash)
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit message:", err)
		os.Exit(1)
	}
	return fmt.Sprintf("%s %s", hash, string(output))
}

func getFilesToReview(config Config, commitInfo CommitInfo) []string {
	// we can use this git log --name-only --pretty=oneline --full-index HEAD^^..HEAD | grep -vE '^[0-9a-f]{40} ' | sort | uniq

	cmd := exec.Command("git", "log", "--name-only", "--pretty=format:", commitInfo.Hash2+".."+commitInfo.Hash1)
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting files to review:", err)
		return []string{}
	}

	// fmt.Println("files to review", string(output))	
	files := strings.Split(string(output), "\n")

	// Filter out non-text files
	validFiles := []string{}
	for _, file := range files {
		// skip empty files
		if (file == "") {
			continue
		}

		if isTextFile(file) {
			validFiles = append(validFiles, file)
		}
	}

	return validFiles


	// prompt := fmt.Sprintf(config.FilesPrompt, commitInfo)

	// response := callLLM(config, config.LowLLM, prompt)

	// // Remove backticks if present
	// response = strings.Trim(response, "`")

	// var files []string
	// err := json.Unmarshal([]byte(response), &files)
	// if err != nil {
	// 	fmt.Println("Error parsing LLM response:", err)
	// 	os.Exit(1)
	// }

	// // Filter out non-text files
	// validFiles := []string{}
	// for _, file := range files {
	// 	if isTextFile(file) {
	// 		validFiles = append(validFiles, file)
	// 	}
	// }

	// return validFiles
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

func loadConfig() Config {
	// Load .env file if it exists
	godotenv.Load()

	webhook := flag.String("webhook", "", "Webhook URL (optional)")
	system := flag.String("system", "", "System prompt")
	filesPrompt := flag.String("files-prompt", "", "Custom files prompt")
	reviewPrompt := flag.String("review-prompt", "", "Custom review prompt")
	envFile := flag.String("env", "", "Path to custom .env file")
	// reviewHash := flag.String("review-hash", "", "Git hash to review (optional)")
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

func (m *multiStringFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

type CommitInfo struct {
	Message string
	Hash1 string
	Hash2 string
}

func getCommitInfo(config Config) CommitInfo {
	var hash1, hash2 string
	// reviewHash := flag.Lookup("review-hash").Value.String()
	reviewHashes := flag.Lookup("review-hashes").Value.(*multiStringFlag)

	if len(*reviewHashes) == 2 {
		hash1, hash2 = (*reviewHashes)[0], (*reviewHashes)[1]
	} else if len(*reviewHashes) == 1 {
		hash1 = (*reviewHashes)[0]
		hash2 = getParentCommit(hash1)
	} else {
		hash1 = getLastCommitHash()
		hash2 = getParentCommit(hash1)
	}

	fmt.Println("Naive reviewing commits:", hash1, hash2)

	commitMessages := getCommitMessages(hash1, hash2, len(*reviewHashes) == 2)

	// we don't even have to check for the commit messages , as we know there are == 2, otherwise we would've exited
	commitMessage := ""
	parts := strings.Split(commitMessages[0], " ")
	hash1 = parts[0]
	commitMessage += parts[1] + "\n"
	parts = strings.Split(commitMessages[1], " ")
	hash2 = parts[0]
	commitMessage += parts[1] + "\n"

	diff := getDiff(hash1, hash2)
	
	fmt.Println("Commits to review:", commitMessages)

	return CommitInfo{
		Message: fmt.Sprintf("Commit: %s\n\nMessage: %s\n\nDiff:\n%s", hash1, commitMessage, diff),
		Hash1:   hash1,
		Hash2:   hash2,
	}
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

func sendWebhook(url string, content string) {
	resp, err := http.Post(url, "text/plain", bytes.NewBufferString(content))
	if err != nil {
		fmt.Println("Error sending webhook:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Webhook sent successfully")
}

func isTextFile(filename string) bool {
	extensions := []string{".txt", ".md", ".go", ".py", ".js", ".html", ".css", ".json", ".xml", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".php"}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, validExt := range extensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

func getCriticalReview(config Config, commitInfo CommitInfo, fileContents map[string]string) string {
	fileContentStr := ""
	for file, content := range fileContents {
		fileContentStr += fmt.Sprintf("\n--- %s ---\n%s\n", file, content)
	}

	prompt := fmt.Sprintf(config.ReviewPrompt, commitInfo.Message, fileContentStr)

	if config.System != "" {
		prompt = config.System + "\n\n" + prompt
	}

	return callLLM(config, config.HighLLM, prompt)
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

func getParentCommit(hash string) string {
	cmd := exec.Command("git", "rev-parse", hash+"^")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting parent commit:", cmd, err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(output))
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

func getCommitMessages(hash1, hash2 string, holdstrict bool) []string {
	var cmd *exec.Cmd
	if (holdstrict) {
		cmd = exec.Command("git", "log", "--pretty=format:%H %s", hash2+"..."+hash1)
	} else {
		cmd = exec.Command("git", "log", "-10", "--pretty=format:%H %s", hash1)
	}
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting commit messages:", err)
		os.Exit(1)
	}

	commits := strings.Split(string(output), "\n")

	if holdstrict {
		return commits 
	}

	var messages strings.Builder

	// go over all commits 
	for _, commit := range commits {
		parts := strings.SplitN(commit, " ", 2)
		if len(parts) == 2 {
			if strings.HasPrefix(parts[1], "Merge ") {
				continue
			}
			messages.WriteString(fmt.Sprintf("%s %s\n", parts[0], parts[1]))
		}
	}

	// now return the top 2 commits; 
	commits = strings.Split(messages.String(), "\n")
	if (len(commits) >= 2) {
		return []string{commits[0], commits[1]}
	}

	log.Fatal("No parent commits found, this must the first commit in the branch")
	os.Exit(0) // not an error

	return []string{}

	
}

func getMergeCommits(mergeHash string) string {
	cmd := exec.Command("git", "log", "--pretty=format:%H %s", mergeHash+"^..."+mergeHash+"^2")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error getting merge commits:", err)
		return ""
	}

	fmt.Println("merge hashes", string(output))
	os.Exit(1)

	commits := strings.Split(string(output), "\n")
	var messages strings.Builder

	for _, commit := range commits {
		parts := strings.SplitN(commit, " ", 2)
		if len(parts) == 2 {
			commitHash := parts[0]
			commitMessage := parts[1]
			if strings.HasPrefix(commitMessage, "Merge ") {
				subMergeCommits := getMergeCommits(commitHash)
				messages.WriteString(fmt.Sprintf("  %s %s\n", commitHash, commitMessage))
				messages.WriteString(subMergeCommits)
			} else {
				messages.WriteString(fmt.Sprintf("  %s %s\n", commitHash, commitMessage))
			}
		}
	}

	return messages.String()
}
