package frontend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"dynamic-pdb/cli/internal/config"
	"dynamic-pdb/cli/internal/dynamicpdbapi"
	"dynamic-pdb/cli/internal/github"
	"dynamic-pdb/cli/internal/paths"
)

func Login(ctx context.Context, stdout, stderr io.Writer) error {
	dataHome, err := paths.DataHome()
	if err != nil {
		return fmt.Errorf("login: locate data directory: %w", err)
	}
	cfg, err := config.Load(dataHome)
	if err != nil {
		return fmt.Errorf("login: load config: %w", err)
	}

	githubClient := github.NewAuthClient(cfg.GitHubClientID())
	dynamicPDBClient := dynamicpdbapi.NewAuthClient(cfg.ServerURL())

	deviceCode, err := githubClient.RequestDeviceCode(ctx)
	if err != nil {
		return fmt.Errorf("login: request GitHub device code: %w", err)
	}
	if _, err := fmt.Fprintf(
		stdout,
		"Open %s in your browser and enter code: %s\n",
		deviceCode.VerificationURI,
		deviceCode.UserCode,
	); err != nil {
		return fmt.Errorf("login: show GitHub device code: %w", err)
	}
	if deviceCode.ExpiresIn > 0 {
		if _, err := fmt.Fprintf(stdout, "(code expires in %s)\n", deviceCode.ExpiresIn.Round(time.Second)); err != nil {
			return fmt.Errorf("login: show GitHub device code expiration: %w", err)
		}
	}

	if _, err := fmt.Fprintf(stdout, "Waiting for authorization...\n"); err != nil {
		return fmt.Errorf("login: show authorization status: %w", err)
	}

	githubToken, err := githubClient.WaitForToken(ctx, deviceCode)
	if err != nil {
		switch {
		case errors.Is(err, github.ErrExpiredToken):
			return fmt.Errorf("login: device code expired; run `dynamic-pdb login` again: %w", err)
		case errors.Is(err, github.ErrAccessDenied):
			return fmt.Errorf("login: GitHub authorization was denied: %w", err)
		default:
			return fmt.Errorf("login: wait for GitHub authorization: %w", err)
		}
	}

	token, err := dynamicPDBClient.ExchangeGitHubToken(ctx, githubToken)
	if err != nil {
		if errors.Is(err, dynamicpdbapi.ErrUnauthorized) {
			return fmt.Errorf("login: GitHub account is not allowed to use Dynamic PDB: %w", err)
		}
		return fmt.Errorf("login: exchange GitHub token with Dynamic PDB: %w", err)
	}

	if err := config.Update(dataHome, func(cfg *config.Config) error {
		cfg.Auth = config.Auth{
			TokenType:   token.TokenType,
			AccessToken: token.AccessToken,
			ExpiresAt:   token.ExpiresAt,
			Name:        token.Name,
			Email:       token.Email,
			Login:       token.Login,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("login: save authentication: %w", err)
	}

	if _, err := fmt.Fprintf(
		stdout,
		"Logged in as @%s. Token expires %s.\n",
		token.Login,
		token.ExpiresAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("login: show login result: %w", err)
	}
	return nil
}

func Logout(stdout io.Writer) error {
	dataHome, err := paths.DataHome()
	if err != nil {
		return fmt.Errorf("logout: locate data directory: %w", err)
	}
	if err := config.ClearAuth(dataHome); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "Logged out.\n"); err != nil {
		return fmt.Errorf("logout: show result: %w", err)
	}
	return nil
}
