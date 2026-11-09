package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"avito-ai-buddy/auth/entity"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type CustomClaims struct {
	UserID    int64  `json:"user_id"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

const (
	accessTokenType  = "access"
	refreshTokenType = "refresh"
	accessTokenTTL   = 15 * time.Minute
	refreshTokenTTL  = 7 * 24 * time.Hour
)

var errMissingJWTSecret = errors.New("jwt secret is empty")

//go:generate mockgen -source=auth.go -destination=mocks/auth_mocks.go -package=mocks
type authRepository interface {
	CreateUser(ctx context.Context, email string, passwordHash string) (user entity.User, err error)
	GetUserByEmail(ctx context.Context, email string) (user entity.User, err error)
}

type authService struct {
	authRepository authRepository
	jwtSecret      []byte
}

func NewAuthService(authRepository authRepository, _ *zap.Logger, jwtSecret []byte) *authService {
	return &authService{
		authRepository: authRepository,
		jwtSecret:      jwtSecret,
	}
}

func (s *authService) Register(ctx context.Context, email, password string) (int64, string, error) {
	if err := validateRegisterInput(email, password); err != nil {
		return 0, "", err
	}

	_, err := s.authRepository.GetUserByEmail(ctx, email)

	if err == nil {
		return 0, "", entity.ErrUserAlreadyExists
	}

	if !errors.Is(err, entity.ErrUserNotFound) {
		return 0, "", fmt.Errorf("check user existence: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, "", fmt.Errorf("hash password: %w", err)
	}

	user, err := s.authRepository.CreateUser(ctx, email, string(hashedPassword))
	if err != nil {
		return 0, "", fmt.Errorf("create user in repo: %w", err)
	}

	return user.ID, "user successfully registered", nil
}

func (s *authService) Login(ctx context.Context, email, password string) (string, string, error) {
	if err := validateRegisterInput(email, password); err != nil {
		return "", "", entity.ErrInvalidCredentials
	}

	user, err := s.authRepository.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return "", "", entity.ErrInvalidCredentials
		}
		return "", "", fmt.Errorf("find user by email: %w", err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", "", entity.ErrInvalidCredentials
		}
		return "", "", fmt.Errorf("compare password hash: %w", err)
	}

	return s.generateTokenPair(user.ID)
}

func (s *authService) ValidateToken(ctx context.Context, tokenString string) (bool, int64, error) {
	userID, err := s.parseToken(tokenString, accessTokenType, accessTokenTTL)
	if err != nil {
		return false, 0, err
	}

	return true, userID, nil
}

func (s *authService) RefreshToken(ctx context.Context, refreshTokenString string) (string, string, error) {
	userID, err := s.parseToken(refreshTokenString, refreshTokenType, refreshTokenTTL)
	if err != nil {
		return "", "", err
	}

	return s.generateTokenPair(userID)
}

func (s *authService) parseToken(tokenString, tokenType string, ttl time.Duration) (int64, error) {
	if len(s.jwtSecret) == 0 {
		return 0, errMissingJWTSecret
	}

	claims := &CustomClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())

	if err != nil || !token.Valid || claims.UserID <= 0 {
		return 0, entity.ErrInvalidToken
	}

	if claims.TokenType == tokenType {
		return claims.UserID, nil
	}

	if claims.TokenType == "" && claims.IssuedAt != nil {
		lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
		if lifetime == ttl || lifetime == ttl-time.Second {
			return claims.UserID, nil
		}
	}

	return 0, entity.ErrInvalidToken
}

func validateRegisterInput(email, password string) error {
	if strings.TrimSpace(email) == "" {
		return entity.ErrInvalidEmail
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return entity.ErrInvalidEmail
	}

	if len(password) > 72 || len([]rune(password)) < 8 {
		return entity.ErrInvalidPassword
	}

	return nil
}

func (s *authService) generateTokenPair(userID int64) (string, string, error) {
	accessToken, err := s.generateToken(userID, accessTokenType, accessTokenTTL)
	if err != nil {
		return "", "", fmt.Errorf("generate access token: %w", err)
	}

	refreshToken, err := s.generateToken(userID, refreshTokenType, refreshTokenTTL)
	if err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	return accessToken, refreshToken, nil
}

func (s *authService) generateToken(userID int64, tokenType string, ttl time.Duration) (string, error) {
	if len(s.jwtSecret) == 0 {
		return "", errMissingJWTSecret
	}

	now := time.Now()
	claims := CustomClaims{
		UserID:    userID,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        rand.Text(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}
