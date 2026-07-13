package main

import "fmt"

// runCompletion prints the completion script for the shell in rest[0].
func runCompletion(rest []string) int {
	if len(rest) < 1 {
		return fail(fmt.Errorf("completion needs a shell argument; supported: bash"))
	}
	switch rest[0] {
	case "bash":
		fmt.Print(bashScript)
		return 0
	}
	return fail(fmt.Errorf("unsupported shell %q; supported: bash", rest[0]))
}

// bashScript completes verbs, flags, files and keys. Keys complete
// level by level like directories: containers end in a dot and the
// word stays open. A word that exactly names a node also offers the
// operator forms (= += $ !). All key knowledge comes from `kv keys`;
// file resolution is never reimplemented in shell.
const bashScript = `# bash completion for kv — github.com/unixfile/kv
# install: . <(kv completion bash)

# key candidates from the resolved file: full dotted paths, containers
# end in a dot and stay open for descent, leaves close the word. $1
# filters the node type (all|leaves|markers); $2=ops adds the operator
# forms when the typed word reaches a node exactly. = and += stay open
# for the value; $ and ! close the command.
_kv_keys() {
    local filter=$1 ops=$2 line out k v prefix=''
    [[ $cur == *.* ]] && prefix=${cur%.*}
    set -- keys
    [[ -n $prefix ]] && set -- keys "$prefix"
    [[ -n $fileflag ]] && set -- -f "$fileflag" "$@"
    out=$("$kvcmd" "$@" 2>/dev/null) || return 0
    local IFS=$'\n'
    for line in $out; do
        [[ $line == "$cur"* ]] || continue
        if [[ $line == *. ]]; then
            [[ $filter == leaves ]] && continue
            COMPREPLY+=("$line")
        else
            [[ $filter == markers ]] && continue
            COMPREPLY+=("$line ")
        fi
    done
    [[ $ops == ops ]] || return 0
    local cands
    for line in $out; do
        # a key holding operator characters only works in verb form
        case $line in *[=+!$]*) continue ;; esac
        k=${line%.}
        [[ $cur == "$k"* ]] || continue
        if [[ $line == *. ]]; then
            cands=("$k " "$k+=" "$k\$ ")
        else
            cands=("$k=" "$k! ")
        fi
        for v in "${cands[@]}"; do
            [[ $v == "$cur"* ]] && COMPREPLY+=("$v")
        done
    done
}

# file candidates for -f/-F: directories and *.kv/*.json first, then
# still-existing entries from the recent list, newest first. Other
# extensions stay reachable by typing; completion encourages the pair
# the resolver knows.
_kv_files() {
    local f
    local -A seen
    local IFS=$'\n'
    for f in $(compgen -f -- "$cur"); do
        seen[$f]=1
        if [[ -d $f ]]; then
            COMPREPLY+=("$f/")
        elif [[ $f == *.kv || $f == *.json ]]; then
            COMPREPLY+=("$f ")
        fi
    done
    local recent=${XDG_STATE_HOME:-$HOME/.local/state}/kv/recent
    [[ -r $recent ]] || return 0
    while IFS= read -r f; do
        [[ -f $f && $f == "$cur"* && -z ${seen[$f]} ]] && COMPREPLY+=("$f ")
    done < "$recent"
}

_kv() {
    local kvcmd=${1:-kv} cur prev verb fileflag arg v i npos
    COMPREPLY=()

    # take the current word from the raw line: bash splits words on =
    # (COMP_WORDBREAKS) and would hide an operator already typed
    cur=${COMP_LINE:0:COMP_POINT}
    cur=${cur##* }

    # past = the cursor is in a value: arbitrary text, nothing to offer
    [[ $cur == *=* ]] && return 0

    verb='' fileflag='' npos=0 i=1
    while ((i < COMP_CWORD)); do
        arg=${COMP_WORDS[i]}
        if [[ $arg == -f || $arg == -F ]]; then
            ((i + 1 < COMP_CWORD)) && fileflag=${COMP_WORDS[i + 1]}
            ((i += 2))
            continue
        fi
        if [[ -z $verb ]]; then
            verb=$arg
        else
            ((npos++))
        fi
        ((i++))
    done
    prev=${COMP_WORDS[COMP_CWORD - 1]}

    if [[ $prev == -f || $prev == -F ]]; then
        _kv_files
        return 0
    fi

    if [[ -z $verb ]]; then
        if [[ $cur == -* ]]; then
            for v in -f -F -F-; do
                [[ $v == "$cur"* ]] && COMPREPLY+=("$v ")
            done
            return 0
        fi
        for v in $("$kvcmd" --verbs 2>/dev/null); do
            [[ $v == "$cur"* ]] && COMPREPLY+=("$v ")
        done
        _kv_keys all ops
        return 0
    fi

    ((npos > 0)) && return 0
    case $verb in
        create | c | push | p | pop | P) _kv_keys markers '' ;;
        update | u | delete | d) _kv_keys leaves '' ;;
        read | r | rmtree | D | keys | k | tree | t | insert | i) _kv_keys all '' ;;
        completion) [[ bash == "$cur"* ]] && COMPREPLY=('bash ') ;;
    esac
    return 0
}

complete -o nospace -o nosort -F _kv kv
`
