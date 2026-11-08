package main

import (
	"context"
	"errors"
	"strings"
)

// Characters 负责人物卡。
type Characters struct{}

var errNameRequired = errors.New("NameRequired")

func (Characters) Save(ctx context.Context, input SaveCharacterInput) (Character, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Character{}, errNameRequired
	}
	id := newID("char")
	if input.ID != nil {
		id = *input.ID
	}
	c := Character{
		ID:           id,
		BookID:       input.BookID,
		Name:         name,
		Description:  input.Description,
		ImageRelPath: input.ImageRelPath,
		CreatedAt:    nowSeconds(),
	}
	if err := upsertCharacter(c); err != nil {
		return Character{}, err
	}
	return c, nil
}

func (Characters) List(ctx context.Context, bookID *string) ([]Character, error) {
	return listCharacters(bookID)
}

func (Characters) Delete(ctx context.Context, id string) error {
	return deleteCharacter(id)
}
