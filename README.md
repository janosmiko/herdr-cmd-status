# herdr-cmd-status

A [Herdr](https://herdr.dev) plugin that shows long shell commands in the Spaces sidebar:

- **Running**: a command that runs longer than 5 seconds (configurable) shows a terminal icon.
- **Finished**: a check icon replaces it when the command ends in a workspace that you do not look at. Selecting the workspace clears it.
- **Notification**: Herdr shows one at the same time, with the exit code if the command failed.

It works for local shells, over `ssh`, and inside VMs such as Lima.

## How it works

The shell writes a mark into the terminal title when a command starts (`⏵ make build`) and when it ends (`⏹ 0 ~/project`). Herdr reads terminal titles from every pane, also through `ssh`. A small watcher process reads the title changes from Herdr's event stream and sets two workspace tokens.

## Requirements

- Herdr 0.9.5 or later
- Node.js 18 or later
- A Nerd Font in the terminal, for the icons
- zsh, or bash 4.4 or later, on each machine that runs commands

## Install

1. Install the plugin:

   ```sh
   herdr plugin install janosmiko/herdr-cmd-status
   ```

2. Add the two cells to the Spaces row in `~/.config/herdr/config.toml`, where you want the icons:

   ```toml
   [ui.sidebar.spaces]
   rows = [[
     "state_icon",
     { token = "$space_cmd_running", fg = "#c78a1f", bold = true },
     { token = "$space_cmd_done", fg = "#4c9a5a", bold = true },
     "workspace",
   ]]
   ```

3. Source the shell snippet at the end of `~/.zshrc` or `~/.bashrc`, locally and on every ssh host or VM:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/janosmiko/herdr-cmd-status/main/shell/cmd-status.zsh -o ~/.cmd-status.zsh
   echo 'source ~/.cmd-status.zsh' >> ~/.zshrc
   ```

   For bash, use `shell/cmd-status.bash` and `~/.bashrc`.

The snippet does nothing outside Herdr, except in ssh sessions, where it cannot tell.

## Configuration

Optional. Create `config.json` in the plugin config directory (`herdr plugin config-dir janosmiko.herdr-cmd-status`), then run the `restart` action:

```json
{
  "delay_seconds": 5,
  "notify": true,
  "skip": ["nvim", "ssh", "claude*"]
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `delay_seconds` | `5` | How long a command runs before it shows as running |
| `notify` | `true` | Show a Herdr notification when a command ends |
| `skip` | editors, pagers, agents, `ssh`, `limactl` | Commands to ignore, as glob patterns. A value replaces the whole list |

## Limits

- **Other title writers:** a program that sets the terminal title while it runs ends the run early. Add it to `skip`.
- **Hook order:** in zsh, the snippet must run as the last `preexec` hook. Source it at the end of `~/.zshrc`.
- **Bash history:** the bash snippet reads the command from history. If `HISTCONTROL` or `HISTIGNORE` keeps a command out of history, the title shows the previous command.
- **Skip list:** only the first word counts, so `sudo nvim` is not skipped.
- **Exit code:** if the prompt title replaces the end mark in the same output burst, the notification says "finished" without the exit code.
- **Restart:** Herdr does not keep tokens across a server restart, so finished icons from before the restart are gone.

## Development

```sh
make help
```

Link a checkout with `herdr plugin link .`, then run the `restart` action after each change to `bin/` or `lib/`.

## License

MIT
