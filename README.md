<p align="center">
  <img src="assets/tuber.png" alt="tuber" width="280"/>
</p>

<p align="center">
  <b>tuber</b> — a light BitTorrent client for the terminal
</p>

<p align="center">
  Magnets · DHT · pause/resume · single-instance handoff<br/>
  Named for sweet potatoes. Built for the keyboard.
</p>

---

## Install

```bash
go install github.com/visnudeva/tuber/cmd/tuber@latest
```

Or from source:

```bash
git clone https://github.com/visnudeva/tuber.git
cd tuber
go build -o tuber ./cmd/tuber
```

## Run

```bash
tuber
tuber -dir ~/Downloads/tuber 'magnet:?xt=urn:btih:…'
```

Downloads default to `~/Downloads/tuber`. Open magnets are restored from `~/.config/tuber/session.json` on the next launch.

### Magnet links

```bash
cp packaging/tuber-open ~/.local/bin/
cp packaging/tuber.desktop ~/.local/share/applications/
# point Exec at your tuber-open, then:
xdg-mime default tuber.desktop x-scheme-handler/magnet
```

## Keys

| Key | Action |
|-----|--------|
| `a` | Add magnet, `.torrent` path, or infohash |
| `p` / `space` | Pause / resume |
| `r` | Remove torrent (keep files) |
| `w` | Wipe torrent and delete files |
| `↑` / `↓` | Move selection |
| `?` | Help |
| `q` | Quit |

## Stack

- [`anacrolix/torrent`](https://github.com/anacrolix/torrent) — BitTorrent engine
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI

## License

MIT
