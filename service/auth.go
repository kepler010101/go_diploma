package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"gophermart/repo"
)

var (
	ErrLoginTaken         = errors.New("login already in use")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
)

type AuthManager struct {
	users  repo.UserRepo
	secret string
}

func NewAuthManager(users repo.UserRepo, secret string) *AuthManager {
	return &AuthManager{users: users, secret: secret}
}

func (m *AuthManager) Register(ctx context.Context, login, password string) (string, error) {
	if strings.TrimSpace(login) == "" || strings.TrimSpace(password) == "" {
		return "", ErrInvalidCredentials
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	user := &repo.User{
		Login:        login,
		PasswordHash: string(hash),
	}
	if err := m.users.CreateUser(ctx, user); err != nil {
		if errors.Is(err, repo.ErrUserExists) {
			return "", ErrLoginTaken
		}
		return "", err
	}

	token := m.tokenForUser(user.ID)
	return token, nil
}

func (m *AuthManager) Login(ctx context.Context, login, password string) (string, error) {
	if strings.TrimSpace(login) == "" || strings.TrimSpace(password) == "" {
		return "", ErrInvalidCredentials
	}

	user, err := m.users.GetByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, repo.ErrUserNotFound) {
			return "", ErrInvalidCredentials
		}
		return "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	token := m.tokenForUser(user.ID)
	return token, nil
}

func (m *AuthManager) ValidateToken(_ context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrInvalidToken
	}

	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, ErrInvalidToken
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return 0, ErrInvalidToken
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ErrInvalidToken
	}

	expected := m.signatureForID(id)
	if parts[1] != expected {
		return 0, ErrInvalidToken
	}

	return id, nil
}

func (m *AuthManager) tokenForUser(id int64) string {
	payload := fmt.Sprintf("%d:%s", id, m.signatureForID(id))
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func (m *AuthManager) signatureForID(id int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", id, m.secret)))
	return fmt.Sprintf("%x", sum[:])
}

var _ AuthService = (*AuthManager)(nil)
