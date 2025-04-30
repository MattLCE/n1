# Contributing to n1

Thank you for your interest in contributing to n1! We welcome contributions from everyone. Whether it's reporting a bug, suggesting a feature, improving documentation, or writing code, your help is appreciated.

This document provides guidelines for contributing to the project.

## Getting Started

### Prerequisites

*   **Go:** Version 1.23 or later (see `go.mod`).
*   **Git:** For version control.
*   **(Optional) Docker:** If using the provided Dev Container.
*   **(Optional) `golangci-lint`:** For running linters locally (installed automatically in dev container).

### Cloning the Repository

```bash
git clone https://github.com/n1/n1.git
cd n1
```

### Development Environment

The easiest way to get a consistent development environment is to use the provided Dev Container configuration with VS Code, GitHub Codespaces, or any compatible tool (like Gitpod via `.gitpod.yml`).

*   **Using Dev Container (Recommended):**
    *   If using VS Code with the "Dev Containers" extension installed, open the cloned repository folder. VS Code should prompt you to "Reopen in Container". Click it.
    *   If using GitHub Codespaces, create a new codespace from the repository.
    *   The container setup (`.devcontainer/devcontainer.json`) installs Go, necessary build tools (like `gcc` for CGO if needed later), `golangci-lint`, and other utilities.
*   **Manual Setup:**
    *   Ensure you have Go installed correctly.
    *   Install `golangci-lint`: See [golangci-lint installation guide](https://golangci-lint.run/usage/install/).
    *   You might need platform-specific build tools (like `gcc`, `libssl-dev`) depending on dependencies or if CGO becomes required later.

### Building the Code

You can build the `bosr` CLI tool using standard Go commands or the Makefile:

```bash
# Using Go
go build -o bin/bosr ./cmd/bosr

# Using Make
make build
```

## Running Checks Locally

Before submitting changes, please run the following checks locally:

*   **Format Code:** Ensure your code is formatted according to Go standards.
    ```bash
    go fmt ./...
    goimports -w . # If you have goimports installed
    ```

*   **Run Unit Tests:** Execute tests within the `internal/` packages.
    ```bash
    go test ./internal/...
    # Or use Make
    make test # Runs go test ./...
    ```

*   **Run Integration Tests:** Execute tests in the `test/` directory, which often involve running the compiled binary.
    ```bash
    # Ensure the binary is built first
    make build
    # Run integration tests (CI flag might enable specific backend behavior)
    CI=true go test -v ./test/...
    ```
    *(Note: The `CI=true` environment variable might be used in tests to simulate the CI environment, e.g., for secret store interaction)*

*   **Run Linter:** Check for style issues and potential errors.
    ```bash
    golangci-lint run ./...
    # Or use Make
    make lint
    ```

*   **Run Vet:** Catch suspicious constructs.
    ```bash
    go vet ./...
    # Or use Make
    make vet
    ```

## Making Changes

1.  **Create a Branch:** Start by creating a new branch off the `main` branch for your feature or fix. Use a descriptive name (e.g., `feat/add-search-flag`, `fix/cleanup-tmp-files`).
    ```bash
    git switch main
    git pull origin main
    git switch -c feat/my-new-feature
    ```
2.  **Implement:** Make your code changes. Remember to:
    *   Follow the coding style and conventions outlined in [4_DECISIONS_CONVENTIONS.md](4_DECISIONS_CONVENTIONS.md).
    *   Add or update tests covering your changes.
    *   Update relevant documentation if you are changing user-facing behavior or architectural components.
3.  **Commit:** Commit your changes using the [Conventional Commits](https://www.conventionalcommits.org/) format (see [4_DECISIONS_CONVENTIONS.md](4_DECISIONS_CONVENTIONS.md#commit-messages)).

## Submitting Pull Requests (PRs)

1.  **Push Branch:** Push your feature branch to your fork on GitHub.
    ```bash
    git push -u origin feat/my-new-feature
    ```
2.  **Open PR:** Go to the n1 repository on GitHub and open a Pull Request from your branch to the `main` branch.
3.  **Describe PR:** Provide a clear title and description for your PR. Explain the changes made and why. If it addresses an existing issue, link it (e.g., `Closes #42`).
4.  **Ensure CI Passes:** GitHub Actions will automatically run the build, lint, and test suite. All checks must pass before the PR can be merged. Address any reported failures.
5.  **Code Review:** Project maintainers will review your PR. Be responsive to feedback and engage in discussion. Updates to your branch will automatically update the PR.
6.  **Merge:** Once approved and CI passes, a maintainer will merge your PR. Congratulations and thank you!

## Reporting Issues

If you find a bug, have a question, or want to suggest a feature, please open an issue on the [GitHub Issues page](https://github.com/n1/n1/issues). Provide as much detail as possible, including steps to reproduce if reporting a bug.

## Code of Conduct

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms. *(Consider adding a standard CODE_OF_CONDUCT.md file if desired)*. We aim for a welcoming and inclusive community.

---

Thank you again for contributing!