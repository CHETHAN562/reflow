package git

import (
	"errors"
	"fmt"
	"github.com/go-git/go-git/v5/plumbing"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"reflow/internal/util"
)

// CloneRepo clones a Git repository to the specified destination path.
// It currently relies on system-configured credentials (SSH agent, credential helpers).
func CloneRepo(repoURL, destPath string) error {
	util.Log.Infof("Cloning repository '%s' into '%s'...", repoURL, destPath)

	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination path '%s' already exists", destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check destination path '%s': %w", destPath, err)
	}

	cloneOptions := &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
		// Depth: 1, // Todo: Decide whether to perform a shallow clone initially. Might be faster.
		// RecurseSubmodules: git.DefaultSubmoduleRecursionDepth, // Handle submodules if needed
	}

	// Attempt to detect SSH key auth automatically using agent or known_hosts
	// This relies on the user having SSH keys configured correctly.
	publicKeysCallback, err := ssh.NewSSHAgentAuth("git")
	if err == nil {
		util.Log.Debug("SSH Agent detected, attempting SSH authentication.")
		cloneOptions.Auth = publicKeysCallback
	} else {
		util.Log.Debugf("SSH Agent not found or failed to initialize, proceeding without explicit SSH auth: %v", err)
	}

	_, err = git.PlainClone(destPath, false, cloneOptions)
	if err != nil {
		util.Log.Errorf("Failed to clone repository '%s': %v", repoURL, err)
		if strings.Contains(err.Error(), "authentication required") {
			util.Log.Error("Authentication failed. Ensure your SSH keys are set up correctly for private repositories or use HTTPS URL with credentials if supported.")
		}
		return fmt.Errorf("failed to clone repository '%s': %w", repoURL, err)
	}

	util.Log.Infof("Successfully cloned repository '%s' to '%s'", repoURL, destPath)
	return nil
}

// FetchUpdates fetches the latest changes from the 'origin' remote for a given repo path.
func FetchUpdates(repoPath string) error {
	util.Log.Debugf("Opening repository at %s", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	util.Log.Infof("Fetching updates for repository at %s...", repoPath)
	fetchOptions := &git.FetchOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
	}

	publicKeysCallback, authErr := ssh.NewSSHAgentAuth("git")
	if authErr == nil {
		util.Log.Debug("SSH Agent detected, attempting SSH authentication for fetch.")
		fetchOptions.Auth = publicKeysCallback
	} else {
		util.Log.Debugf("SSH Agent not found for fetch, proceeding without explicit SSH auth: %v", authErr)
	}

	err = repo.Fetch(fetchOptions)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		util.Log.Errorf("Failed to fetch updates for repository '%s': %v", repoPath, err)
		if strings.Contains(err.Error(), "authentication required") {
			util.Log.Error("Authentication failed during fetch. Ensure SSH keys or credentials are valid.")
		}
		return fmt.Errorf("failed to fetch updates for repository '%s': %w", repoPath, err)
	}

	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		util.Log.Infof("Repository '%s' is already up-to-date.", repoPath)
	} else {
		util.Log.Infof("Successfully fetched updates for repository '%s'", repoPath)
	}
	return nil
}

// CheckoutCommit checks out a specific commit hash or branch in the repository.
func CheckoutCommit(repoPath, commitHashOrBranch string) error {
	util.Log.Debugf("Opening repository at %s", repoPath)
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree for repository at %s: %w", repoPath, err)
	}

	util.Log.Infof("Checking out '%s' in repository '%s'...", commitHashOrBranch, repoPath)

	checkoutOptions := &git.CheckoutOptions{
		Create: false,
		Force:  false,
	}

	hash, err := repo.ResolveRevision(plumbing.Revision(commitHashOrBranch))
	if err != nil {
		util.Log.Errorf("Failed to resolve revision '%s': %v", commitHashOrBranch, err)
		return fmt.Errorf("failed to resolve revision '%s': %w", commitHashOrBranch, err)
	}

	util.Log.Debugf("Resolved '%s' to commit hash: %s", commitHashOrBranch, hash.String())
	checkoutOptions.Hash = *hash

	err = w.Checkout(checkoutOptions)
	if err != nil {
		util.Log.Errorf("Failed to checkout '%s' in repository '%s': %v", commitHashOrBranch, repoPath, err)
		return fmt.Errorf("failed to checkout '%s': %w", commitHashOrBranch, err)
	}

	util.Log.Infof("Successfully checked out '%s' (commit: %s)", commitHashOrBranch, hash.String()[:7])
	return nil
}
