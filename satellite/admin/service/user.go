package service

import (
	"context"
	"net/mail"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	"golang.org/x/crypto/bcrypt"

	"storj.io/storj/satellite/console"
)

// User is an object that describes user
type User struct {
	UserID    uuid.UUID `json:"userId"`
	Email     string    `json:"email"`
	FullName  string    `json:"fullName"`
	ShortName string    `json:"shortName"`
	CreatedAt time.Time `json:"createdAt"`
	PartnerID uuid.UUID `json:"partnerId"`
}

// GetUser is a method for querying user by id
func (s *Service) GetUser(ctx context.Context, userID *uuid.UUID) (*User, error) {
	u, err := s.consoleDB.Users().Get(ctx, *userID)
	if err != nil {
		return nil, errs.New(userDoesNotExistErrMsg)
	}

	return mapUser(u), nil
}

// GetUserByEmail is a method for querying user by email
func (s *Service) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u, err := s.consoleDB.Users().GetByEmail(ctx, email)
	if err != nil {
		return nil, errs.New(userDoesNotExistErrMsg)
	}

	return mapUser(u), nil
}

// CreateUser creates a new user
func (s *Service) CreateUser(
	ctx context.Context,
	email string,
	fullName string,
	password string,
	shortName string,
	createdAt time.Time,
	partnerID string,
) (*User, error) {

	if err := isValid(&email, &fullName, &password, &partnerID); err != nil {
		return nil, err
	}

	u, err := s.consoleDB.Users().GetByEmail(ctx, email)
	if err == nil {
		return nil, errs.New(emailUsedErrMsg)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.passwordCost)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	// store data
	err = s.consoleDB.WithTx(ctx, func(ctx context.Context, tx console.DBTx) error {
		userID, err := uuid.New()
		if err != nil {
			return Error.Wrap(err)
		}

		newUser := &console.User{
			ID:           *userID,
			Email:        email,
			FullName:     fullName,
			ShortName:    shortName,
			PasswordHash: hash,
			Status:       console.Inactive,
		}
		if partnerID != "" {
			partnerID, err := uuid.Parse(partnerID)
			if err != nil {
				return Error.Wrap(err)
			}
			newUser.PartnerID = *partnerID
		}

		u, err = tx.Users().Insert(ctx,
			newUser,
		)
		if err != nil {
			return Error.Wrap(err)
		}

		regToken, err := tx.RegistrationTokens().Create(ctx, 1)
		if err != nil {
			return Error.Wrap(err)
		}
		err = tx.RegistrationTokens().UpdateOwner(ctx, regToken.Secret, u.ID)
		if err != nil {
			return Error.Wrap(err)
		}

		u.Status = console.Active
		err = tx.Users().Update(ctx, u)
		if err != nil {
			return Error.Wrap(err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return mapUser(u), nil
}

// UpdateUser creates a new user
func (s *Service) UpdateUser(
	ctx context.Context,
	userID uuid.UUID,
	email *string,
	fullName *string,
	password *string,
	shortName *string,
) (*User, error) {

	u, err := s.consoleDB.Users().Get(ctx, userID)
	if err != nil {
		return nil, errs.New(userDoesNotExistErrMsg)
	}

	if err := isValid(email, fullName, password, nil); err != nil {
		return nil, err
	}

	if nil != email {
		userCollision, err := s.consoleDB.Users().GetByEmail(ctx, *email)
		if err == nil && u.ID != userCollision.ID {
			return nil, errs.New(emailUsedErrMsg)
		}
		u.Email = *email
	}

	if nil != fullName {
		u.FullName = *fullName
	}

	if nil != password {
		hash, err := bcrypt.GenerateFromPassword([]byte(*password), s.passwordCost)
		if err != nil {
			return nil, Error.Wrap(err)
		}
		u.PasswordHash = hash
	}

	if nil != shortName {
		u.ShortName = *shortName
	}

	err = s.consoleDB.Users().Update(ctx, u)

	if err != nil {
		return nil, err
	}

	return mapUser(u), nil
}

func mapUser(u *console.User) *User {
	return &User{
		UserID:    u.ID,
		Email:     u.Email,
		FullName:  u.FullName,
		ShortName: u.ShortName,
		CreatedAt: u.CreatedAt,
		PartnerID: u.PartnerID,
	}
}

// isValid checks new user validity and returns error describing whats wrong.
func isValid(
	email *string,
	fullName *string,
	password *string,
	partnerID *string,
) error {

	var errs validationErrors

	if nil != fullName {
		errs.AddWrap(ValidateFullName(*fullName))
	}

	if nil != password {
		errs.AddWrap(ValidatePassword(*password))
	}

	if nil != email {
		// validate email
		_, err := mail.ParseAddress(*email)
		errs.AddWrap(err)
	}

	if nil != partnerID && *partnerID != "" {
		_, err := uuid.Parse(*partnerID)
		if err != nil {
			errs.AddWrap(err)
		}
	}

	return errs.Combine()
}
