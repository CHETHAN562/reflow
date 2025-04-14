# Reflow

**A simple CLI deployment manager for Next.js apps using Docker (Nginx, Blue-Green) on Linux VPS.**

Reflow aims to simplify the process of deploying Next.js applications to your own Linux server with zero downtime, handling the complexities of building, containerizing, and routing traffic.

[![Go Version](https://img.shields.io/badge/Go-1.18+-blue?logo=go&logoColor=white)](https://golang.org/dl/)
## Introduction

Deploying modern web applications often involves managing Docker containers, configuring reverse proxies like Nginx, and ensuring smooth updates without interrupting users. Reflow streamlines this for Next.js applications by providing a command-line interface to manage the entire lifecycle on a single Linux server, implementing a blue-green deployment strategy.

It automatically builds your Next.js app within Docker (no need for a Dockerfile in your project!), manages Docker containers, configures Nginx as a reverse proxy, and handles switching traffic between deployment slots.

## Features

* **Zero-Downtime Deployments:** Uses a Blue/Green strategy to ensure your application remains available during updates.
* **Simple CLI Interface:** Manage deployments with straightforward commands (`reflow deploy`, `reflow approve`, etc.).
* **Automated Next.js Builds:** No need to write or maintain a `Dockerfile` in your application repository. Reflow generates one based on your configuration.
* **Test and Production Environments:** Deploy to a staging/test URL first, then promote to production with manual approval.
* **Deploy Specific Commits:** Rollback or deploy any commit from your Git repository history.
* **Managed Nginx:** Runs and configures Nginx in a Docker container (`reflow-nginx`) to act as a reverse proxy.
* **Easy Setup:** Initialize the required server environment with `reflow init`.
* **Project Management:** Create, list, and view the status of your deployed projects (`reflow project create|list|status`).
* **Lifecycle Control:** Start and stop active application containers (`reflow project start|stop`).
* **Log Viewing:** Stream or view logs from your application containers (`reflow project logs`).
* **Resource Cleanup:** Remove inactive deployment containers and optionally old images (`reflow project cleanup`).
* **Configuration Management:** View or edit project configurations easily (`reflow project config view|edit`).
* **Basic Health Checks:** Performs a TCP port check to verify container readiness before switching traffic.
* **Simple Rollback on Failure:** Automatically attempts to clean up containers from failed deployments/approvals.

## Requirements

* A Linux-based VPS or dedicated server.
* **Docker:** Installed and the Docker daemon running. The user running `reflow` needs permission to interact with the Docker socket (e.g., being part of the `docker` group).
* **Git:** The `git` command-line tool must be installed and available in the system's PATH.
* **Go:** Required only if building from source (min version 1.18+ recommended - *Update if specific version needed*).

## Installation

### From Source

1.  **Install Go:** Make sure you have Go installed (version 1.18+ recommended).
2.  **Clone the repository:**
    ```bash
    git clone https://github.com/RevereInc/reflow.git
    cd reflow
    ```
3.  **Build the binary:**
    ```bash
    go build -o reflow .
    ```
    This will create the executable `reflow` in the current directory.
4.  **(Optional) Move to PATH:** Move the `reflow` binary to a directory in your system's PATH (e.g., `/usr/local/bin`) for easier access:
    ```bash
    sudo mv reflow /usr/local/bin/
    ```

### Install Script (Linux)

You can install the latest version of Reflow using the following command. It automatically detects your architecture (amd64/arm64), downloads the correct release binary, and installs it to `/usr/local/bin`.

**Note:** This requires `curl` or `wget`, `tar`, and `sudo` privileges to write to `/usr/local/bin`.

```bash
curl -sSL https://raw.githubusercontent.com/RevereInc/reflow/main/install.sh | sudo bash
```

## Getting Started / Usage

1.  **Initialize Reflow:**
    Navigate to the directory where you want Reflow to store its runtime data (or run it where you want the `./reflow` directory created).
    ```bash
    reflow init
    ```
    * This creates the `./reflow` directory structure (`apps/`, `nginx/`, etc.).
    * It creates the `reflow-network` Docker network.
    * It starts the `reflow-nginx` container.
    * It creates a default global configuration at `./reflow/config.yaml`.
    * **Important:** Edit `./reflow/config.yaml` and set `defaultDomain` to your actual domain name.

2.  **Create a Project:**
    ```bash
    reflow project create <project-name> <github-repo-url> [flags]
    ```
    * `<project-name>`: An internal name for your project (e.g., `my-blog`).
    * `<github-repo-url>`: The HTTPS or SSH URL for your Git repository.
    * Flags:
        * `--test-domain <domain>`: Override the calculated test domain (default: `<project-name>-test.<defaultDomain>`).
        * `--prod-domain <domain>`: Override the calculated production domain (default: `<project-name>-prod.<defaultDomain>`).
    * This clones the repo into `./reflow/apps/<project-name>/repo/` and creates `./reflow/apps/<project-name>/config.yaml` and `state.json`.
    * You may want to edit `./reflow/apps/<project-name>/config.yaml` using `reflow project config edit <project-name>` to adjust the `nodeVersion`, `appPort`, or `envFile` paths if they differ from the defaults.

3.  **Deploy to Test Environment:**
    ```bash
    reflow deploy <project-name> [commit-ish]
    ```
    * Deploys the specified Git commit, tag, or branch (or `HEAD` of the default branch if omitted) to the test environment.
    * Builds the Docker image, starts a container, performs a health check, updates Nginx, and saves the state.
    * Outputs the likely URL for the test environment (e.g., `http://<project-name>-test.<defaultDomain>`). **Remember to set up DNS for this domain to point to your server's IP!**

4.  **Approve for Production:**
    (After verifying the test deployment works)
    ```bash
    reflow approve <project-name>
    ```
    * Promotes the exact version currently running in the `test` environment to `prod`.
    * Uses the *same Docker image* built during the `deploy` step.
    * Starts a container in the inactive production slot, performs a health check, updates Nginx for the production domain, and saves the state.
    * Outputs the likely URL for the production environment. **Ensure DNS is set up for the production domain!**

5.  **Other Commands:**
    * List projects: `reflow project list`
    * Detailed status: `reflow project status <project-name>`
    * View logs: `reflow project logs <project-name> --env <test|prod> [-f] [--tail N]`
    * Stop app: `reflow project stop <project-name> --env <test|prod|all>`
    * Start app: `reflow project start <project-name> --env <test|prod|all>`
    * Cleanup old deployments: `reflow project cleanup <project-name> [--env <env>] [--prune-images]` (Use `--prune-images` with caution)
    * View config: `reflow project config view <project-name>`
    * Edit config: `reflow project config edit <project-name>`
    * **Destroy Everything:** `reflow destroy [--force]` (Use with extreme caution!)

## Configuration

* **Global:** `./reflow/config.yaml`
    * `defaultDomain`: Used to calculate environment URLs if not overridden (e.g., `example.com`).
    * `debug`: Set to `true` for verbose logging (can also use `--debug` flag).
* **Project:** `./reflow/apps/<project-name>/config.yaml`
    * `githubRepo`: URL of the repository.
    * `appPort`: The port your Next.js app listens on inside the container (default: `3000`).
    * `nodeVersion`: The Docker Node.js image tag to use for building and running (e.g., `"18-alpine"`, `"20-slim"`). Default: `"18-alpine"`.
    * `environments`: Map defining `test` and `prod`.
        * `domain`: Specific domain for the environment (overrides default calculation).
        * `envFile`: Path *relative to the repository root* for the `.env` file to load for this environment (e.g., `.env.production`).

## Health Checks

Reflow performs a basic health check after starting a new container during `deploy` and `approve`. It verifies that the container is accepting TCP connections on its configured `appPort`. This check runs from within the Nginx container over the Docker network. While this ensures the process has started listening, it doesn't guarantee full application readiness.

## Rollback

Reflow includes a simple automatic rollback mechanism. If the `deploy` or `approve` process fails *after* starting the new application container but *before* successfully switching traffic and saving state (e.g., due to a failed health check or Nginx reload error), Reflow will attempt to stop and remove the container it just started for that failed attempt. This helps prevent leaving broken containers running.

An explicit `reflow rollback <project> --env <env>` command is not currently implemented but is a potential future addition.

## Automatic Deployments (CI/CD Integration)

Reflow itself does not watch your Git repository. To automate deployments on pushes (e.g., push to `main` deploys to `test`), you need to configure your CI/CD platform (like GitHub Actions).

**Example GitHub Action Workflow Snippet:**

```yaml
name: Deploy to Test

on:
  push:
    branches:
      - main # Or your default branch

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy via SSH
        uses: appleboy/ssh-action@v1.0.3 # Or your preferred SSH action
        with:
          host: ${{ secrets.SSH_HOST }}
          username: ${{ secrets.SSH_USERNAME }}
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          script: |
            cd /path/to/where/t/is || exit 1
            ./reflow deploy your-project-name ${{ github.sha }}
```

* You need to add `SSH_HOST`, `SSH_USERNAME`, and `SSH_PRIVATE_KEY` as secrets in your GitHub repository settings.
* Replace `your-project-name` with the name you used in `reflow project create`.
* Ensure the user connecting via SSH has permissions to run `reflow` and interact with Docker.

## Contributing

Contributions are welcome! Please feel free to open issues or submit pull requests via the [GitHub repository](https://github.com/RevereInc/reflow).

1.  Fork the repository.
2.  Create a new branch (`git checkout -b feat/your-feature-name`).
3.  Make your changes.
4.  Ensure code is formatted (`go fmt ./...`).
5.  Commit your changes (`git commit -am 'feat: added some feature'`).
6.  Push to the branch (`git push origin feat/your-feature-name`).
7.  Open a new Pull Request against the main repository.

## License

Copyright (c) 2025 Revere

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to view, use, and contribute to the Software, subject to the following conditions:

1. Attribution: The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.  
2. No Sale: You may not sell the Software itself, either in its original form or bundled with other software, without explicit permission from the copyright holder. Using the Software as a tool within your own commercial operations is permitted.
3. Modification and Distribution: You may fork the repository and modify the Software for your own private, personal use only. You may not publish or distribute your modified version of the Software, in whole or in part, publicly or privately, except by contributing changes back to the original repository via Pull Requests. You may not sublicense the Software under a different name or brand.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
