package docker

import (
	"context"
	"fmt"
	"reflow/internal/util"

	"github.com/docker/docker/api/types/image"
	dockerAPIClient "github.com/docker/docker/client"
)

// FindImage checks if an image with the given reference string exists locally.
func FindImage(ctx context.Context, imageRef string) (*image.Summary, error) {
	cli, err := GetClient()
	if err != nil {
		return nil, err
	}

	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == imageRef {
				util.Log.Debugf("Found existing image %s (ID: %s)", imageRef, img.ID)
				foundImg := img
				return &foundImg, nil
			}
		}
	}

	util.Log.Debugf("Image %s not found locally", imageRef)
	return nil, nil
}

// RemoveImage removes an image by its ID.
func RemoveImage(ctx context.Context, imageID string) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}

	util.Log.Infof("Removing image %s...", imageID)
	options := image.RemoveOptions{Force: false, PruneChildren: true}
	_, err = cli.ImageRemove(ctx, imageID, options)
	if err != nil {
		if dockerAPIClient.IsErrNotFound(err) {
			util.Log.Warnf("Image %s not found, cannot remove.", imageID)
			return nil
		}
		util.Log.Errorf("Failed to remove image %s: %v", imageID, err)
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}
	util.Log.Infof("Successfully removed image %s", imageID)
	return nil
}
