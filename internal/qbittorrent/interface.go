package qbittorrent

import "context"

type QBTClient interface {
	Login(ctx context.Context, username, password string) error
	GetTorrentsInfo(ctx context.Context) ([]TorrentInfo, error)
	GetTorrentInfo(ctx context.Context, hash string) (*TorrentInfo, error)
	AddTorrent(ctx context.Context, magnetURI string) error
	DeleteTorrent(ctx context.Context, hash string, deleteFiles bool) error
	Ping(ctx context.Context) error
}
