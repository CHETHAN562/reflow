package util

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// FindEditor determines the editor command to use.
func FindEditor() (string, error) {
	editor := os.Getenv("EDITOR")
	if editor != "" {
		Log.Debugf("Using editor from $EDITOR: %s", editor)
		return editor, nil
	}

	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{"notepad"}
	} else {
		candidates = []string{"nano"}
	}

	Log.Debugf("$EDITOR not set, trying candidates: %v", candidates)
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err == nil {
			Log.Debugf("Found editor: %s", path)
			return path, nil
		}
	}

	return "", fmt.Errorf("cannot find a suitable editor: $EDITOR environment variable is not set and common editors (vim, nano, vi) not found in PATH")
}

// OpenFileInEditor opens the specified file path in the user's preferred editor.
func OpenFileInEditor(filePath string) error {
	editorCmd, err := FindEditor()
	if err != nil {
		return err
	}

	Log.Infof("Opening %s in editor %s...", filePath, editorCmd)

	cmd := exec.Command(editorCmd, filePath)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		Log.Errorf("Editor command '%s' failed: %v", editorCmd, err)
		return fmt.Errorf("editor command failed: %w", err)
	}

	Log.Infof("Editor closed for %s", filePath)
	return nil
}
