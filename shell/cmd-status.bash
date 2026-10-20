# herdr-cmd-status: writes command start and end marks into the terminal title.
# Source it at the end of ~/.bashrc. Needs bash 4.4 or later for PS0.

[[ -n $HERDR_ENV || -n $SSH_CONNECTION ]] || return 0
[[ $PS0 == *__hcs_run* ]] && return 0

# PS0 has no access to the command line, so read it back from history.
__hcs_run() {
  local line
  line=$(HISTTIMEFORMAT='' history 1)
  [[ $line =~ ^[[:space:]]*[0-9]+\*?[[:space:]]+(.*)$ ]] && line=${BASH_REMATCH[1]}
  # A BEL or ESC in the command would end the title sequence early.
  line=${line//[[:cntrl:]]/ }
  printf '\e]2;\xe2\x8f\xb5 %s\a' "${line:0:120}"
}

__hcs_done() {
  local status=$?
  printf '\e]2;\xe2\x8f\xb9 %d %s\a' "$status" "${PWD/#$HOME/\~}"
  return "$status"
}

# shellcheck disable=SC2016 # bash expands PS0 each time it prints it
PS0+='$(__hcs_run)'
# First, so it sees the command's exit status.
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == "declare -a"* ]]; then
  PROMPT_COMMAND=(__hcs_done "${PROMPT_COMMAND[@]}")
else
  # shellcheck disable=SC2128,SC2178 # this branch only runs when PROMPT_COMMAND is a string
  PROMPT_COMMAND="__hcs_done${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
fi
