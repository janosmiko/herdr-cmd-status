# herdr-cmd-status: writes command start and end marks into the terminal title.
# Source it at the end of ~/.zshrc. The start mark must be the last title write
# before the command runs, so no other preexec hook may run after this one.

[[ -n $HERDR_ENV || -n $SSH_CONNECTION ]] || return 0

__hcs_preexec() {
  printf '\e]2;%s %s\a' $'⏵' "${${1//[[:cntrl:]]/ }[1,120]}"
}

__hcs_precmd() {
  printf '\e]2;%s %d %s\a' $'⏹' $? "${PWD/#$HOME/~}"
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec __hcs_preexec
# First, so the shell's own prompt title replaces the end mark.
precmd_functions=(__hcs_precmd ${precmd_functions:#__hcs_precmd})
