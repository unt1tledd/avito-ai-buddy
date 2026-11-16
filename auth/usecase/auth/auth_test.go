package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"avito-ai-buddy/auth/entity"
	"avito-ai-buddy/auth/usecase/auth/mocks"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

var (
	testJWTSecret = []byte("super_secret_key")
)

func TestAuthService_Register_Success_Gomock(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	// Arrange
	authRepo := mocks.NewMockauthRepository(ctrl)
	logger := zap.NewNop()

	srv := NewAuthService(authRepo, logger, testJWTSecret)

	email := "test@example.com"
	password := "password123"
	expectedUserID := int64(42)

	authRepo.EXPECT().
		GetUserByEmail(gomock.Any(), email).
		Return(entity.User{}, entity.ErrUserNotFound)

	authRepo.EXPECT().
		CreateUser(gomock.Any(), email, gomock.Any()).
		Return(entity.User{ID: expectedUserID, Email: email}, nil)

	// Act
	userID, msg, err := srv.Register(context.Background(), email, password)

	// Assert
	require.NoError(t, err)
	require.Equal(t, expectedUserID, userID)
	require.Equal(t, "user successfully registered", msg)
}

func TestAuthService_Register_Err_Gomock(t *testing.T) {
	t.Parallel()

	repoErr := errors.New("db connection error")

	tests := []struct {
		name        string
		email       string
		password    string
		mockSetup   func(authRepo *mocks.MockauthRepository)
		expectedErr error
	}{
		{
			name:        "invalid email",
			email:       "invalid-email",
			password:    "password123",
			mockSetup:   func(authRepo *mocks.MockauthRepository) {},
			expectedErr: entity.ErrInvalidEmail,
		},
		{
			name:        "short password",
			email:       "test@example.com",
			password:    "short",
			mockSetup:   func(authRepo *mocks.MockauthRepository) {},
			expectedErr: entity.ErrInvalidPassword,
		},
		{
			name:        "password exceeds bcrypt byte limit",
			email:       "test@example.com",
			password:    strings.Repeat("я", 37),
			mockSetup:   func(authRepo *mocks.MockauthRepository) {},
			expectedErr: entity.ErrInvalidPassword,
		},
		{
			name:     "user already exists",
			email:    "test@example.com",
			password: "password123",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{ID: 1}, nil)
			},
			expectedErr: entity.ErrUserAlreadyExists,
		},
		{
			name:     "get user repo error",
			email:    "test@example.com",
			password: "password123",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{}, repoErr)
			},
			expectedErr: repoErr,
		},
		{
			name:     "create user repo error",
			email:    "test@example.com",
			password: "password123",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{}, entity.ErrUserNotFound)

				authRepo.EXPECT().
					CreateUser(gomock.Any(), "test@example.com", gomock.Any()).
					Return(entity.User{}, repoErr)
			},
			expectedErr: repoErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			authRepo := mocks.NewMockauthRepository(ctrl)
			logger := zap.NewNop()

			srv := NewAuthService(authRepo, logger, testJWTSecret)

			tt.mockSetup(authRepo)

			userID, msg, err := srv.Register(context.Background(), tt.email, tt.password)

			require.Error(t, err)
			require.EqualValues(t, 0, userID)
			require.Empty(t, msg)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestAuthService_Login_Success_Gomock(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	// Arrange
	authRepo := mocks.NewMockauthRepository(ctrl)
	logger := zap.NewNop()

	srv := NewAuthService(authRepo, logger, testJWTSecret)

	email := "test@example.com"
	password := "password123"
	expectedUserID := int64(777)

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	authRepo.EXPECT().
		GetUserByEmail(gomock.Any(), email).
		Return(entity.User{ID: expectedUserID, Email: email, PasswordHash: string(hashedPassword)}, nil)
	var storedHash []byte
	var storedExpiry time.Time
	authRepo.EXPECT().CreateRefreshToken(gomock.Any(), expectedUserID, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, tokenHash []byte, expiresAt time.Time) error {
			storedHash, storedExpiry = tokenHash, expiresAt
			return nil
		})

	// Act
	accessToken, refreshToken, err := srv.Login(context.Background(), email, password)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, accessToken)
	require.NotEmpty(t, refreshToken)
	expectedHash := sha256.Sum256([]byte(refreshToken))
	require.Equal(t, expectedHash[:], storedHash)
	claims := &CustomClaims{}
	_, err = jwt.ParseWithClaims(refreshToken, claims, func(*jwt.Token) (any, error) {
		return testJWTSecret, nil
	})
	require.NoError(t, err)
	require.True(t, storedExpiry.Equal(claims.ExpiresAt.Time))
}

func TestAuthService_Login_Err_Gomock(t *testing.T) {
	t.Parallel()

	repoErr := errors.New("db error")

	validHash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)

	tests := []struct {
		name        string
		email       string
		password    string
		mockSetup   func(authRepo *mocks.MockauthRepository)
		expectedErr error
	}{
		{
			name:        "invalid input format",
			email:       "invalid",
			password:    "pass1234",
			mockSetup:   func(authRepo *mocks.MockauthRepository) {},
			expectedErr: entity.ErrInvalidCredentials,
		},
		{
			name:        "password exceeds bcrypt byte limit",
			email:       "test@example.com",
			password:    strings.Repeat("я", 37),
			mockSetup:   func(authRepo *mocks.MockauthRepository) {},
			expectedErr: entity.ErrInvalidCredentials,
		},
		{
			name:     "user not found",
			email:    "test@example.com",
			password: "password123",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{}, entity.ErrUserNotFound)
			},
			expectedErr: entity.ErrInvalidCredentials,
		},
		{
			name:     "repo error on get user",
			email:    "test@example.com",
			password: "password123",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{}, repoErr)
			},
			expectedErr: repoErr,
		},
		{
			name:     "repo error on store refresh token",
			email:    "test@example.com",
			password: "correct-password",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{ID: 1, PasswordHash: string(validHash)}, nil)
				authRepo.EXPECT().CreateRefreshToken(gomock.Any(), int64(1), gomock.Any(), gomock.Any()).Return(repoErr)
			},
			expectedErr: repoErr,
		},
		{
			name:     "wrong password",
			email:    "test@example.com",
			password: "wrong-password",
			mockSetup: func(authRepo *mocks.MockauthRepository) {
				authRepo.EXPECT().
					GetUserByEmail(gomock.Any(), "test@example.com").
					Return(entity.User{ID: 1, PasswordHash: string(validHash)}, nil)
			},
			expectedErr: entity.ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			authRepo := mocks.NewMockauthRepository(ctrl)
			logger := zap.NewNop()

			srv := NewAuthService(authRepo, logger, testJWTSecret)

			tt.mockSetup(authRepo)

			accessToken, refreshToken, err := srv.Login(context.Background(), tt.email, tt.password)

			require.Error(t, err)
			require.Empty(t, accessToken)
			require.Empty(t, refreshToken)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestAuthService_ValidateToken_Success(t *testing.T) {
	t.Parallel()

	srv := NewAuthService(nil, zap.NewNop(), testJWTSecret)
	expectedUserID := int64(10)

	tokenString, err := srv.generateToken(expectedUserID, accessTokenType, accessTokenTTL, time.Now())
	require.NoError(t, err)

	// Act
	isValid, userID, err := srv.ValidateToken(context.Background(), tokenString)

	// Assert
	require.NoError(t, err)
	require.True(t, isValid)
	require.Equal(t, expectedUserID, userID)
}

func TestAuthService_ValidateToken_Err(t *testing.T) {
	t.Parallel()

	srv := NewAuthService(nil, zap.NewNop(), testJWTSecret)

	expiredToken, err := srv.generateToken(1, accessTokenType, -time.Minute, time.Now())
	require.NoError(t, err)

	refreshToken, err := srv.generateToken(1, refreshTokenType, refreshTokenTTL, time.Now())
	require.NoError(t, err)

	claims := CustomClaims{UserID: 1, TokenType: accessTokenType}
	noExpiration, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	require.NoError(t, err)
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(accessTokenTTL))
	wrongAlgorithm, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString(testJWTSecret)
	require.NoError(t, err)
	noUserID, err := srv.generateToken(0, accessTokenType, accessTokenTTL, time.Now())
	require.NoError(t, err)

	tests := []struct {
		name        string
		token       string
		expectedErr error
	}{
		{
			name:        "empty token",
			token:       "",
			expectedErr: entity.ErrInvalidToken,
		},
		{
			name:        "invalid token format",
			token:       "invalid.token.string",
			expectedErr: entity.ErrInvalidToken,
		},
		{
			name:        "expired token",
			token:       expiredToken,
			expectedErr: entity.ErrInvalidToken,
		},
		{name: "refresh token", token: refreshToken, expectedErr: entity.ErrInvalidToken},
		{name: "missing expiration", token: noExpiration, expectedErr: entity.ErrInvalidToken},
		{name: "unexpected algorithm", token: wrongAlgorithm, expectedErr: entity.ErrInvalidToken},
		{name: "missing user id", token: noUserID, expectedErr: entity.ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid, userID, err := srv.ValidateToken(context.Background(), tt.token)

			require.Error(t, err)
			require.False(t, isValid)
			require.EqualValues(t, 0, userID)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestAuthService_RefreshToken_Success(t *testing.T) {
	t.Parallel()

	repo := mocks.NewMockauthRepository(gomock.NewController(t))
	srv := NewAuthService(repo, zap.NewNop(), testJWTSecret)
	expectedUserID := int64(99)

	oldRefreshToken, err := srv.generateToken(expectedUserID, refreshTokenType, refreshTokenTTL, time.Now())
	require.NoError(t, err)
	oldHash := sha256.Sum256([]byte(oldRefreshToken))
	var storedHash []byte
	gomock.InOrder(
		repo.EXPECT().RotateRefreshToken(gomock.Any(), expectedUserID, oldHash[:], gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ int64, _, newHash []byte, _ time.Time) (bool, error) {
				storedHash = newHash
				return true, nil
			}),
		repo.EXPECT().RotateRefreshToken(gomock.Any(), expectedUserID, oldHash[:], gomock.Any(), gomock.Any()).Return(false, nil),
	)

	// Act
	newAccessToken, newRefreshToken, err := srv.RefreshToken(context.Background(), oldRefreshToken)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, newAccessToken)
	require.NotEqual(t, oldRefreshToken, newRefreshToken)
	newHash := sha256.Sum256([]byte(newRefreshToken))
	require.Equal(t, newHash[:], storedHash)

	access, refresh, err := srv.RefreshToken(t.Context(), oldRefreshToken)
	require.ErrorIs(t, err, entity.ErrInvalidToken)
	require.Empty(t, access)
	require.Empty(t, refresh)

	valid, userID, err := srv.ValidateToken(t.Context(), newAccessToken)
	require.NoError(t, err)
	require.True(t, valid)
	require.Equal(t, expectedUserID, userID)

	_, _, err = srv.RefreshToken(t.Context(), newAccessToken)
	require.ErrorIs(t, err, entity.ErrInvalidToken)
}

func TestAuthService_LegacyTokens(t *testing.T) {
	t.Parallel()

	repo := mocks.NewMockauthRepository(gomock.NewController(t))
	srv := NewAuthService(repo, zap.NewNop(), testJWTSecret)
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": 42,
		"iat":     now.Unix(),
		"exp":     now.Add(accessTokenTTL - time.Second).Unix(),
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	require.NoError(t, err)
	claims["exp"] = now.Add(refreshTokenTTL).Unix()
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testJWTSecret)
	require.NoError(t, err)

	valid, userID, err := srv.ValidateToken(t.Context(), accessToken)
	require.NoError(t, err)
	require.True(t, valid)
	require.EqualValues(t, 42, userID)

	refreshHash := sha256.Sum256([]byte(refreshToken))
	repo.EXPECT().RotateRefreshToken(gomock.Any(), int64(42), refreshHash[:], gomock.Any(), gomock.Any()).Return(false, nil)
	_, _, err = srv.RefreshToken(t.Context(), refreshToken)
	require.ErrorIs(t, err, entity.ErrInvalidToken)

	_, _, err = srv.ValidateToken(t.Context(), refreshToken)
	require.ErrorIs(t, err, entity.ErrInvalidToken)
	_, _, err = srv.RefreshToken(t.Context(), accessToken)
	require.ErrorIs(t, err, entity.ErrInvalidToken)
}

func TestAuthService_RejectsEmptyJWTSecret(t *testing.T) {
	t.Parallel()

	srv := NewAuthService(nil, zap.NewNop(), nil)
	_, _, err := srv.generateTokenPair(1, time.Now())
	require.ErrorIs(t, err, errMissingJWTSecret)

	claims := jwt.MapClaims{"user_id": 1, "token_type": accessTokenType, "exp": time.Now().Add(accessTokenTTL).Unix()}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte{})
	require.NoError(t, err)
	valid, _, err := srv.ValidateToken(t.Context(), token)
	require.ErrorIs(t, err, errMissingJWTSecret)
	require.False(t, valid)
}
