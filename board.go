package main

import (
	"context"
	"errors"
	"strings"
)

// Board 是画布服务；一块画布的数据类型叫 BoardData。
type Board struct{}

var errEmptyName = errors.New("EmptyName")

func (Board) Load(ctx context.Context, id string) (*BoardData, error) {
	return getBoard(id)
}

func (Board) Save(ctx context.Context, board BoardData) error {
	board.UpdatedAt = nowSeconds()
	return saveBoard(board)
}

func (Board) List(ctx context.Context) ([]BoardSummary, error) {
	return listBoards()
}

func (Board) Create(ctx context.Context, name string) (BoardData, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "新画布"
	}
	b := BoardData{
		ID:           newID("board"),
		Name:         trimmed,
		ViewportJSON: `{"x":0,"y":0,"zoom":1}`,
		NodesJSON:    "[]",
		EdgesJSON:    "[]",
		UpdatedAt:    nowSeconds(),
	}
	if err := saveBoard(b); err != nil {
		return BoardData{}, err
	}
	return b, nil
}

func (Board) Rename(ctx context.Context, id, name string) (BoardSummary, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return BoardSummary{}, errEmptyName
	}
	updatedAt := nowSeconds()
	if err := renameBoard(id, trimmed, updatedAt); err != nil {
		return BoardSummary{}, err
	}
	return BoardSummary{ID: id, Name: trimmed, UpdatedAt: updatedAt}, nil
}

func (Board) Delete(ctx context.Context, id string) error {
	return deleteBoard(id)
}
