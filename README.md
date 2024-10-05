# 🔍 Git Commit Review Tool 🛠️

This Go program automates the process of reviewing Git commits using AI language models. It analyzes the latest commit, selects files for review, and provides a comprehensive critique of the changes.

## 🌟 Features

- 📊 Analyzes the latest Git commit
- 🤖 Uses AI to select files for review
- 📝 Generates detailed code reviews
- 🔒 Supports environment variables and .env files
- 🎛️ Customizable prompts via text files
- 🖨️ Prints review results to stdout
- 🚀 Optionally sends review results to a webhook
- 🖥️ Cross-compilation support for Linux AMD64

## 🛠️ Installation

1. Clone this repository:

   ```
   git clone https://github.com/yourusername/git-commit-review-tool.git
   cd git-commit-review-tool
   ```

2. Install dependencies:
   ```
   go mod tidy
   ```

## ⚙️ Configuration

Set up the following environment variables or use a `.env` file:

- `OR_BASE`: Base URL for the AI model API
- `OR_TOKEN`: API token for authentication
- `OR_LOW`: Model name for the low-cost AI (used for file selection)
- `OR_HIGH`: Model name for the high-cost AI (used for detailed review)

## 📋 Usage

Run the program with the following command:

```
go run main.go [options]
```

### 🚩 Options

- `-webhook` (optional): URL to send the review results (webhook)
- `-system` (optional): System prompt to prepend to the review prompt
- `-files-prompt` (optional): Path to a custom files prompt (default: `filesprompt.txt`)
- `-review-prompt` (optional): Path to a custom review prompt (default: `reviewprompt.txt`)
- `-env` (optional): Path to a custom .env file

## 📄 Customizing Prompts

- Edit `filesprompt.txt` to customize the prompt for file selection
- Edit `reviewprompt.txt` to customize the prompt for the detailed review

## 🏗️ Building

Use the provided Makefile to build the project:

```
make build
```

To build for Linux AMD64:

```
make build-linux-amd64
```

## 🧪 Testing

Run tests using:

```
make test
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgements

- OpenAI for providing the language models
- The Go community for the excellent libraries and tools

Happy reviewing! 🎉