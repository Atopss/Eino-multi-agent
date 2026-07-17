package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userKey contextKey = "eino_user"

// Claims 是 JWT 的 payload。
type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"usr"`
	jwt.RegisteredClaims
}

// UserInfo 当前登录用户（注入到 context）。
type UserInfo struct {
	UserID   string
	Username string
}

func WithUser(ctx context.Context, userID, username string) context.Context {
	return context.WithValue(ctx, userKey, &UserInfo{UserID: userID, Username: username})
}

func UserFromContext(ctx context.Context) (*UserInfo, bool) {
	u, ok := ctx.Value(userKey).(*UserInfo)
	return u, ok
}

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func SignToken(userID, username, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ParseToken(token, secret string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// UserStore 封装 users 表的读写。
type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

func (s *UserStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *UserStore) Create(username, password string, isAdmin bool) (string, error) {
	id := newID()
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(
		`INSERT INTO users(id, username, password_hash, is_admin, created_at) VALUES(?,?,?,?,?)`,
		id, username, hash, boolToInt(isAdmin), time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// Verify 校验用户名密码，成功返回用户。
func (s *UserStore) Verify(username, password string) (*UserInfo, error) {
	var id, hash string
	var isAdmin int
	err := s.db.QueryRow(
		`SELECT id, password_hash, is_admin FROM users WHERE username = ?`, username,
	).Scan(&id, &hash, &isAdmin)
	if err == sql.ErrNoRows {
		return nil, errors.New("invalid credentials")
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(hash, password) {
		return nil, errors.New("invalid credentials")
	}
	return &UserInfo{UserID: id, Username: username}, nil
}

// IsAdmin 判断用户是否为管理员（用于敏感端点的二次授权）。
func (s *UserStore) IsAdmin(userID string) (bool, error) {
	var isAdmin int
	err := s.db.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if err != nil {
		return false, err
	}
	return isAdmin == 1, nil
}

// EnsureAdmin 数据库为空时，用环境变量（或默认 admin/admin）创建初始管理员。
// 注意：默认凭据仅用于本地首次启动，生产部署应通过 INIT_ADMIN_USERNAME/PASSWORD 覆盖。
func (s *UserStore) EnsureAdmin() {
	n, err := s.Count()
	if err != nil {
		log.Printf("检查管理员失败: %v", err)
		return
	}
	if n > 0 {
		return
	}
	username := getEnv("INIT_ADMIN_USERNAME", "admin")
	password := os.Getenv("INIT_ADMIN_PASSWORD")
	if password == "" {
		// 未显式设置密码：随机生成强密码并打印一次性提示，
		// 避免默认 admin/admin 弱口令在联网部署时暴露。
		password = generateRandomPassword(16)
		log.Printf("已为初始管理员 %s 生成随机密码（请妥善保存，仅显示一次）：%s", username, password)
	}
	if _, err := s.Create(username, password, true); err != nil {
		log.Printf("创建初始管理员失败: %v", err)
		return
	}
	log.Printf("已创建初始管理员账号 (username=%s)。请尽快修改默认密码！", username)
}

// generateRandomPassword 用 crypto/rand 生成不含易混淆字符的随机密码。
func generateRandomPassword(n int) string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "Chg" + time.Now().Format("20060102150405")
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102150405") + hex.EncodeToString([]byte{byte(time.Now().Nanosecond())})
	}
	return hex.EncodeToString(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
