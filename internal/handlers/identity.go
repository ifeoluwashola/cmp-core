// internal/handlers/identity.go
// HTTP handlers for user registration and login.

package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ifeoluwashola/cmp-core/internal/auth"
	"github.com/ifeoluwashola/cmp-core/internal/database"
	"github.com/ifeoluwashola/cmp-core/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IdentityHandler wires the auth utilities and repository for identity operations.
type IdentityHandler struct {
	pool *pgxpool.Pool
	repo *repository.IdentityRepository
	jwt  *auth.Manager
}

// NewIdentityHandler constructs the handler.
func NewIdentityHandler(pool *pgxpool.Pool, jwt *auth.Manager) *IdentityHandler {
	return &IdentityHandler{
		pool: pool,
		repo: repository.NewIdentityRepository(),
		jwt:  jwt,
	}
}

// ─── POST /register ───────────────────────────────────────────────────────────

type registerRequest struct {
	OrganizationName string `json:"organization_name" binding:"required"`
	Email            string `json:"email"             binding:"required,email"`
	Password         string `json:"password"          binding:"required,min=8"`
}

type registerResponse struct {
	Organization struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Tier string `json:"tier"`
	} `json:"organization"`
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
}

// Register handles POST /register.
//
//	@Summary  Register a new organization and owner account
//	@Tags     identity
//	@Accept   json
//	@Produce  json
//	@Param    body  body      registerRequest   true  "Registration details"
//	@Success  201   {object}  registerResponse
//	@Failure  400   {object}  map[string]string
//	@Failure  409   {object}  map[string]string
//	@Failure  500   {object}  map[string]string
//	@Router   /register [post]
func (h *IdentityHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	var resp registerResponse
	err = database.WithServiceTx(c.Request.Context(), h.pool, func(tx pgx.Tx) error {
		org, user, txErr := h.repo.CreateOrganizationWithAdmin(
			c.Request.Context(), tx,
			req.OrganizationName, req.Email, hash,
		)
		if txErr != nil {
			return txErr
		}
		resp.Organization.ID = org.ID.String()
		resp.Organization.Name = org.Name
		resp.Organization.Tier = org.Tier
		resp.User.ID = user.ID.String()
		resp.User.Email = user.Email
		resp.User.Role = string(user.Role)
		return nil
	})
	if err != nil {
		// Detect unique constraint violation on email
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// ─── POST /login ──────────────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	Token string `json:"token"`
}

// Login handles POST /login.
//
//	@Summary  Authenticate and receive a JWT
//	@Tags     identity
//	@Accept   json
//	@Produce  json
//	@Param    body  body      loginRequest   true  "Credentials"
//	@Success  200   {object}  loginResponse
//	@Failure  400   {object}  map[string]string
//	@Failure  401   {object}  map[string]string
//	@Failure  500   {object}  map[string]string
//	@Router   /login [post]
func (h *IdentityHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var token string
	err := database.WithServiceTx(c.Request.Context(), h.pool, func(tx pgx.Tx) error {
		user, txErr := h.repo.GetUserByEmail(c.Request.Context(), tx, req.Email)
		if txErr != nil {
			return txErr
		}

		if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
			return fmt.Errorf("invalid credentials")
		}

		token, txErr = h.jwt.Generate(user.ID, user.OrganizationID, string(user.Role))
		return txErr
	})
	if err != nil {
		if err.Error() == "invalid credentials" || err.Error() == "identity: user not found" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, loginResponse{Token: token})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// isUniqueViolation checks for a PostgreSQL unique constraint error (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return len(err.Error()) > 0 &&
		(contains(err.Error(), "23505") || contains(err.Error(), "unique constraint"))
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
