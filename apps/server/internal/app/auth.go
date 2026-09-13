package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brantje/agent-board/apps/server/internal/store"
	"golang.org/x/crypto/argon2"
)

const (
	authIssuer        = "agent-board"
	authAudience      = "agent-board-web"
	passwordTokenTTL  = 24 * time.Hour
	accessClockSkew   = 2 * time.Minute
	randomTokenBytes  = 32
	passwordSaltBytes = 16
	argonTime         = uint32(2)
	argonMemoryKiB    = uint32(19 * 1024)
	argonThreads      = uint8(1)
	argonKeyLength    = uint32(32)
	dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$YWdlbnQtYm9hcmQtZHVtbQ$60YCWxb2Pw+vr0ewbP9Ek5/RTYsRn9Ek+rvE7mcQ3Qk"
)

var (
	errAuthenticationFailed = errors.New("authentication failed")
	errInvalidAccessToken   = errors.New("invalid access token")
	errInvalidPasswordToken = errors.New("invalid password token")
)

type AuthService struct {
	store      store.AuthStore
	now        func() time.Time
	random     io.Reader
	signingKey []byte
}

type AuthServiceConfig struct {
	Now        func() time.Time
	Random     io.Reader
	SigningKey []byte
}

type BootstrapRegistration struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

type AuthenticatedUser struct {
	ID                  string
	Username            string
	Email               string
	DisplayName         string
	DeploymentRole      string
	Status              string
	ForcePasswordChange bool
	AuthVersion         int64 `json:"-"`
}

type AuthTokens struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
	User                  AuthenticatedUser
}

type PasswordTokenSecret struct {
	Token     string
	ExpiresAt time.Time
}

type accessClaims struct {
	Subject     string `json:"sub"`
	AuthVersion int64  `json:"av"`
	Issuer      string `json:"iss"`
	Audience    string `json:"aud"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

type accessHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func NewAuthService(authStore store.AuthStore, config AuthServiceConfig) (*AuthService, error) {
	if authStore == nil {
		return nil, fmt.Errorf("auth store is required")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	random := config.Random
	if random == nil {
		random = rand.Reader
	}
	key := append([]byte(nil), config.SigningKey...)
	if len(key) == 0 {
		key = make([]byte, randomTokenBytes)
		if _, err := io.ReadFull(random, key); err != nil {
			return nil, fmt.Errorf("generate auth signing key: %w", err)
		}
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("auth signing key must be at least 32 bytes")
	}
	return &AuthService{store: authStore, now: now, random: random, signingKey: key}, nil
}

func (s *AuthService) BootstrapAvailable(ctx context.Context) (bool, error) {
	count, err := s.store.UserCount(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *AuthService) Bootstrap(ctx context.Context, input BootstrapRegistration) (AuthenticatedUser, error) {
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	username, email, displayName, err := normalizeIdentity(input.Username, input.Email, input.DisplayName)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	if err := ValidatePassword(settings.PasswordPolicy, input.Password); err != nil {
		return AuthenticatedUser{}, err
	}
	hash, err := s.hashPassword(input.Password)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	user, err := s.store.BootstrapUser(ctx, store.User{
		Username:       username,
		Email:          email,
		DisplayName:    displayName,
		PasswordHash:   hash,
		DeploymentRole: store.DeploymentRoleAdmin,
		Status:         store.UserStatusActive,
		AuthVersion:    1,
	})
	if errors.Is(err, store.ErrConflict) {
		return AuthenticatedUser{}, NewError("bootstrap_closed", "bootstrap registration is unavailable", err)
	}
	if err != nil {
		return AuthenticatedUser{}, err
	}
	return publicUser(user), nil
}

func (s *AuthService) Login(ctx context.Context, login, password string) (AuthTokens, error) {
	login = normalizeLogin(login)
	if login == "" || password == "" {
		return AuthTokens{}, authFailure()
	}
	user, lookupErr := s.store.GetUserByLogin(ctx, login)
	eligible := lookupErr == nil && user.Status == store.UserStatusActive && user.PasswordHash != ""
	passwordHash := dummyPasswordHash
	if eligible {
		passwordHash = user.PasswordHash
	}
	ok, verifyErr := verifyPassword(passwordHash, password)
	if !eligible || verifyErr != nil || !ok {
		return AuthTokens{}, authFailure()
	}
	return s.startSession(ctx, user)
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (AuthTokens, error) {
	oldHash, err := hashOpaqueToken(refreshToken)
	if err != nil {
		return AuthTokens{}, authFailure()
	}
	session, err := s.store.GetAuthSessionByRefreshHash(ctx, oldHash)
	if err != nil {
		return AuthTokens{}, authFailure()
	}
	now := s.now().UTC()
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return AuthTokens{}, authFailure()
	}
	user, err := s.store.GetUser(ctx, session.UserID)
	if err != nil || user.Status != store.UserStatusActive {
		return AuthTokens{}, authFailure()
	}
	newToken, newHash, err := s.newOpaqueToken()
	if err != nil {
		return AuthTokens{}, err
	}
	if _, err := s.store.RotateAuthSession(ctx, session.ID, oldHash, newHash, session.ExpiresAt, now); err != nil {
		return AuthTokens{}, authFailure()
	}
	accessToken, accessExpiry, err := s.issueAccessToken(ctx, user, now)
	if err != nil {
		return AuthTokens{}, err
	}
	return AuthTokens{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiry,
		RefreshToken:          newToken,
		RefreshTokenExpiresAt: session.ExpiresAt,
		User:                  publicUser(user),
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	hash, err := hashOpaqueToken(refreshToken)
	if err != nil {
		return nil
	}
	err = s.store.RevokeAuthSessionByRefreshHash(ctx, hash, s.now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

func (s *AuthService) AuthenticateAccessToken(ctx context.Context, token string) (AuthenticatedUser, error) {
	claims, err := s.parseAccessToken(token)
	if err != nil {
		return AuthenticatedUser{}, authFailure()
	}
	user, err := s.store.GetUser(ctx, claims.Subject)
	if err != nil || user.Status != store.UserStatusActive || user.AuthVersion != claims.AuthVersion {
		return AuthenticatedUser{}, authFailure()
	}
	return publicUser(user), nil
}

func (s *AuthService) CreatePasswordToken(ctx context.Context, userID, purpose string) (PasswordTokenSecret, error) {
	if purpose != store.PasswordTokenPurposeSetup && purpose != store.PasswordTokenPurposeReset {
		return PasswordTokenSecret{}, NewError("invalid_argument", "password token purpose is invalid", store.ErrInvalidArgument)
	}
	if _, err := s.store.GetUser(ctx, userID); err != nil {
		return PasswordTokenSecret{}, err
	}
	raw, hash, err := s.newOpaqueToken()
	if err != nil {
		return PasswordTokenSecret{}, err
	}
	expiresAt := s.now().UTC().Add(passwordTokenTTL)
	if _, err := s.store.CreatePasswordToken(ctx, store.PasswordToken{
		UserID:    userID,
		Purpose:   purpose,
		TokenHash: hash,
		ExpiresAt: expiresAt,
	}); err != nil {
		return PasswordTokenSecret{}, err
	}
	return PasswordTokenSecret{Token: raw, ExpiresAt: expiresAt}, nil
}

func (s *AuthService) CompletePasswordToken(ctx context.Context, rawToken, purpose, password string) (AuthenticatedUser, error) {
	if purpose != store.PasswordTokenPurposeSetup && purpose != store.PasswordTokenPurposeReset {
		return AuthenticatedUser{}, NewError("invalid_argument", "password token purpose is invalid", store.ErrInvalidArgument)
	}
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	if err := ValidatePassword(settings.PasswordPolicy, password); err != nil {
		return AuthenticatedUser{}, err
	}
	tokenHash, err := hashOpaqueToken(rawToken)
	if err != nil {
		return AuthenticatedUser{}, passwordTokenFailure()
	}
	passwordHash, err := s.hashPassword(password)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	user, err := s.store.CompletePasswordToken(ctx, tokenHash, purpose, passwordHash, s.now().UTC())
	if err != nil {
		return AuthenticatedUser{}, passwordTokenFailure()
	}
	return publicUser(user), nil
}

func (s *AuthService) SetPassword(ctx context.Context, userID, password string, forcePasswordChange bool) (AuthenticatedUser, error) {
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	if err := ValidatePassword(settings.PasswordPolicy, password); err != nil {
		return AuthenticatedUser{}, err
	}
	hash, err := s.hashPassword(password)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	user, err := s.store.SetUserPassword(ctx, userID, hash, forcePasswordChange)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	return publicUser(user), nil
}

func (s *AuthService) SetStatus(ctx context.Context, userID, status string) (AuthenticatedUser, error) {
	if status != store.UserStatusPending && status != store.UserStatusActive && status != store.UserStatusDisabled {
		return AuthenticatedUser{}, NewError("invalid_argument", "user status is invalid", store.ErrInvalidArgument)
	}
	user, err := s.store.SetUserStatus(ctx, userID, status)
	if err != nil {
		return AuthenticatedUser{}, err
	}
	return publicUser(user), nil
}

func (s *AuthService) startSession(ctx context.Context, user store.User) (AuthTokens, error) {
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return AuthTokens{}, err
	}
	if err := validateAuthSettings(settings); err != nil {
		return AuthTokens{}, err
	}
	now := s.now().UTC()
	refreshToken, refreshHash, err := s.newOpaqueToken()
	if err != nil {
		return AuthTokens{}, err
	}
	refreshExpiry := now.Add(settings.RefreshTokenLifetime)
	if _, err := s.store.CreateAuthSession(ctx, store.AuthSession{
		UserID:           user.ID,
		RefreshTokenHash: refreshHash,
		ExpiresAt:        refreshExpiry,
	}); err != nil {
		return AuthTokens{}, err
	}
	accessToken, accessExpiry, err := s.issueAccessTokenWithSettings(user, now, settings)
	if err != nil {
		_ = s.store.RevokeAuthSessionByRefreshHash(ctx, refreshHash, now)
		return AuthTokens{}, err
	}
	return AuthTokens{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiry,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiry,
		User:                  publicUser(user),
	}, nil
}

func (s *AuthService) issueAccessToken(ctx context.Context, user store.User, now time.Time) (string, time.Time, error) {
	settings, err := s.store.GetAuthSettings(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	return s.issueAccessTokenWithSettings(user, now, settings)
}

func (s *AuthService) issueAccessTokenWithSettings(user store.User, now time.Time, settings store.AuthSettings) (string, time.Time, error) {
	if err := validateAuthSettings(settings); err != nil {
		return "", time.Time{}, err
	}
	expiresAt := now.Add(settings.AccessTokenLifetime)
	claims := accessClaims{
		Subject:     user.ID,
		AuthVersion: user.AuthVersion,
		Issuer:      authIssuer,
		Audience:    authAudience,
		IssuedAt:    now.Unix(),
		ExpiresAt:   expiresAt.Unix(),
	}
	token, err := signAccessToken(s.signingKey, claims)
	return token, expiresAt, err
}

func (s *AuthService) parseAccessToken(token string) (accessClaims, error) {
	claims, err := verifyAccessToken(s.signingKey, token)
	if err != nil {
		return accessClaims{}, errInvalidAccessToken
	}
	now := s.now().UTC()
	if claims.Subject == "" || claims.AuthVersion < 1 || claims.Issuer != authIssuer || claims.Audience != authAudience {
		return accessClaims{}, errInvalidAccessToken
	}
	if claims.ExpiresAt <= now.Unix() || claims.IssuedAt > now.Add(accessClockSkew).Unix() {
		return accessClaims{}, errInvalidAccessToken
	}
	return claims, nil
}

func (s *AuthService) newOpaqueToken() (string, []byte, error) {
	raw := make([]byte, randomTokenBytes)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", nil, fmt.Errorf("generate authentication token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], nil
}

func (s *AuthService) hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := io.ReadFull(s.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(encoded, password string) (bool, error) {
	var version int
	var memory, iterations uint32
	var parallelism uint8
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("invalid password hash")
	}
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, fmt.Errorf("invalid password hash")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("invalid password hash")
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 16 {
		return false, fmt.Errorf("invalid password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, fmt.Errorf("invalid password hash")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false, fmt.Errorf("invalid password hash")
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func ValidatePassword(policy store.PasswordPolicy, password string) error {
	if err := validatePasswordPolicy(policy); err != nil {
		return err
	}
	if utf8.RuneCountInString(password) < policy.MinimumLength {
		return passwordPolicyFailure("password is shorter than the configured minimum length")
	}
	var upper, lower, number, symbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsNumber(r):
			number = true
		case !unicode.IsSpace(r):
			symbol = true
		}
	}
	if policy.RequireUppercase && !upper {
		return passwordPolicyFailure("password must contain an uppercase letter")
	}
	if policy.RequireLowercase && !lower {
		return passwordPolicyFailure("password must contain a lowercase letter")
	}
	if policy.RequireNumber && !number {
		return passwordPolicyFailure("password must contain a number")
	}
	if policy.RequireSymbol && !symbol {
		return passwordPolicyFailure("password must contain a symbol")
	}
	return nil
}

func validatePasswordPolicy(policy store.PasswordPolicy) error {
	if policy.MinimumLength < 8 || policy.MinimumLength > 256 {
		return NewError("auth_settings_invalid", "password policy is invalid", store.ErrInvalidArgument)
	}
	return nil
}

func validateAuthSettings(settings store.AuthSettings) error {
	if settings.AccessTokenLifetime < 5*time.Minute || settings.AccessTokenLifetime > 24*time.Hour {
		return NewError("auth_settings_invalid", "access token lifetime is invalid", store.ErrInvalidArgument)
	}
	if settings.RefreshTokenLifetime < time.Hour || settings.RefreshTokenLifetime > 365*24*time.Hour {
		return NewError("auth_settings_invalid", "refresh token lifetime is invalid", store.ErrInvalidArgument)
	}
	return validatePasswordPolicy(settings.PasswordPolicy)
}

func normalizeIdentity(username, email, displayName string) (string, string, string, error) {
	username = normalizeLogin(username)
	email = normalizeLogin(email)
	displayName = strings.TrimSpace(displayName)
	if username == "" || email == "" || displayName == "" {
		return "", "", "", NewError("invalid_argument", "username, email and displayName are required", store.ErrInvalidArgument)
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsed.Address, email) || parsed.Name != "" {
		return "", "", "", NewError("invalid_argument", "email is invalid", store.ErrInvalidArgument)
	}
	return username, email, displayName, nil
}

func normalizeLogin(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func hashOpaqueToken(token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errAuthenticationFailed
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != randomTokenBytes {
		return nil, errAuthenticationFailed
	}
	digest := sha256.Sum256([]byte(token))
	return digest[:], nil
}

func publicUser(user store.User) AuthenticatedUser {
	return AuthenticatedUser{
		ID:                  user.ID,
		Username:            user.Username,
		Email:               user.Email,
		DisplayName:         user.DisplayName,
		DeploymentRole:      user.DeploymentRole,
		Status:              user.Status,
		ForcePasswordChange: user.ForcePasswordChange,
		AuthVersion:         user.AuthVersion,
	}
}

func authFailure() error {
	return NewError("authentication_failed", "authentication failed", errAuthenticationFailed)
}

func passwordTokenFailure() error {
	return NewError("password_token_invalid", "password token is invalid or expired", errInvalidPasswordToken)
}

func passwordPolicyFailure(message string) error {
	return NewError("password_policy", message, store.ErrInvalidArgument)
}

func signAccessToken(key []byte, claims accessClaims) (string, error) {
	headerJSON, err := json.Marshal(accessHeader{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	message := header + "." + payload
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return message + "." + signature, nil
}

func verifyAccessToken(key []byte, token string) (accessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return accessClaims{}, errInvalidAccessToken
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return accessClaims{}, errInvalidAccessToken
	}
	var header accessHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return accessClaims{}, errInvalidAccessToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return accessClaims{}, errInvalidAccessToken
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return accessClaims{}, errInvalidAccessToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessClaims{}, errInvalidAccessToken
	}
	var claims accessClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return accessClaims{}, errInvalidAccessToken
	}
	return claims, nil
}
