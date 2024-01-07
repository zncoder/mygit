package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/zncoder/check"
)

type OpList struct{} // placeholder to help discover methods

func checkoutBranch(br string, revertBuf bool) {
	sh("git checkout %s", br)
	if revertBuf {
		revertEmacsBuffers()
	}
}

func revertEmacsBuffers() {
	shQ("emacsclient -e '(my-revert-unmodified)'")
}

func sh(s string, args ...any) string {
	return shellCmd(s, false, args...)
}

func shQ(s string, args ...any) string {
	return shellCmd(s, true, args...)
}

var verbose = flag.Bool("v", false, "show commands")

func shellCmd(s string, ignoreErr bool, args ...any) string {
	if len(args) > 0 {
		s = fmt.Sprintf(s, args...)
	}

	if *verbose {
		log.Println(s)
	}
	c := exec.Command("/bin/sh", "-c", s)
	if *verbose {
		c.Stderr = os.Stderr
	}
	b := check.V(c.Output()).I(ignoreErr).F("run", "args", c.Args[2])
	return string(bytes.TrimSpace(b))
}

var (
	repoDir,
	curBranch,
	mainBranch,
	repoBranch,
	mainWorktreeDir,
	username string
)

func RepoDir() string {
	if repoDir == "" {
		repoDir = shQ("git rev-parse --show-toplevel")
	}
	return repoDir
}

func getCurBranch() string {
	br := sh("git rev-parse --abbrev-ref HEAD")
	if br == "HEAD" {
		br = sh("git rev-parse --short HEAD")
	}
	return br
}

func CurBranch() string {
	if curBranch == "" {
		curBranch = getCurBranch()
	}
	return curBranch
}

func Username() string {
	if username == "" {
		u := check.Vp(user.Current())
		username = u.Username
	}
	return username
}

func MainBranch() string {
	if mainBranch == "" {
		mainBranch = sh(`git branch -l main master --format '%(refname:short)'`)
	}
	return mainBranch
}

func RepoBranch() string {
	rd := RepoDir()
	bd := filepath.Base(rd)
	if strings.HasPrefix(bd, "wt-") {
		return bd
	}
	return MainBranch()
}

func MainWorktreeDir() string {
	if mainWorktreeDir == "" {
		var dir string
		s := sh("git worktree list")
		lns := strings.Split(s, "\n")
		for _, ln := range lns {
			if strings.Contains(ln, "/wt-") {
				continue
			}
			check.T(dir == "").F("main worktree not unique", "worktree_list", lns)
			dir = strings.Fields(ln)[0]
		}
		return dir
	}
	return mainWorktreeDir
}

func localBranch(pat string, inUse bool) string {
	brs := matchLocalBranches(pat, inUse, false)
	check.T(len(brs) == 1).F("not unique branch", "pattern", pat, "local_branches", brs)
	return brs[0]
}

const tmpSuffix = "__TMP"

func matchLocalBranches(pat string, inUse, tmp bool) []string {
	var brs []string
	re := regexp.MustCompile(pat)
	s := sh("git branch")
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "*") {
			if !inUse {
				continue
			}
			ln = strings.TrimSpace(ln[1:])
		}
		if !tmp && strings.HasSuffix(ln, tmpSuffix) {
			continue
		}
		if !re.MatchString(ln) {
			continue
		}
		brs = append(brs, ln)
	}
	return brs
}

func remoteBranch(pat string) string {
	brs := matchRemoteBranches(pat, false, false)
	check.T(len(brs) == 1).F("not unique remote branch", "pattern", pat, "remote_branches", brs)
	return brs[0]
}

func matchRemoteBranches(pat string, mine, tmp bool) []string {
	var brs []string
	re := regexp.MustCompile(pat)
	s := sh("git branch -r")
	minePrefix := fmt.Sprintf("origin/%s", Username())
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if mine && !strings.HasPrefix(ln, minePrefix) {
			continue
		}
		if !tmp && strings.HasSuffix(ln, tmpSuffix) {
			continue
		}
		if !re.MatchString(ln) {
			continue
		}
		brs = append(brs, strings.TrimPrefix(ln, "origin/"))
	}
	return brs
}

var commitRe = regexp.MustCompile(`^[0-9a-f]{6}[0-9a-f]*$`)

func isCommit(s string) bool {
	return strings.HasPrefix(s, "HEAD") || commitRe.MatchString(s)
}

func pullMain() {
	log.Println("pull main")
	sh("git fetch --prune --tags")
	bc := CurBranch()
	bm := MainBranch()
	br := RepoBranch()
	wd := check.Vp(os.Getwd())
	onWt := br != bm
	if onWt {
		mwd := MainWorktreeDir()
		log.Printf("cd main dir:%s", mwd)
		os.Chdir(mwd)
	}
	if bc != bm {
		log.Printf("switch to main:%s", bm)
		checkoutBranch(bm, false)
	}

	cb := getCurBranch()
	check.T(cb == bm).F("not in main branch", "current_branch", cb, "main_branch", bm)
	log.Printf("pull in %s", bm)
	sh("git pull --rebase")

	if onWt {
		log.Printf("cd worktree dir: %s", wd)
		os.Chdir(wd)
	}
	if bc != br {
		log.Printf("switch to worktree: %s", br)
		checkoutBranch(br, false)
		log.Printf("pull in %s", br)
		sh("git rebase %s", bm)
	}

	if bc != bm || bc != br {
		checkoutBranch(bc, false)
	}
}

func (op OpList) BoCheckoutLocalBranch() {
	revertBuf := flag.Bool("r", false, "revert emacs buffers")
	parseFlag("[branch_re]")

	var br string
	if flag.NArg() == 0 {
		br = RepoBranch()
	} else {
		br = localBranch(flag.Arg(0), false)
	}

	bc := CurBranch()
	check.T(bc != br).F("already in branch", "current", bc)
	log.Printf("branch %s -> %s", bc, br)
	checkoutBranch(br, *revertBuf)
}

func (op OpList) BcCheckoutCommit() {
	parseFlag("commit_or_tag")
	cot := flag.Arg(0)
	if isCommit(cot) {
		sh("git checkout --detach %s", cot)
	} else {
		sh("git checkout tags/%s -b %s", cot, cot)
	}
}

func (op OpList) BtCheckoutAndTrackRemoteBranch() {
	parseFlag("remote_branch_re")
	br := remoteBranch(flag.Arg(0))
	sh("git checkout -b %s --track origin/%s", br, br)
}

func (op OpList) BnNewBranch() {
	parseFlag("branch_name", "[base_branch_re_or_dot]")

	br := flag.Arg(0)
	check.T(!strings.Contains(br, "/")).F("branch name cannot contain `/`", "branch", br)
	br = fmt.Sprintf("%s/%s", Username(), br)

	var bb string
	if flag.NArg() > 1 {
		if flag.Arg(1) != "." {
			bb = localBranch(flag.Arg(1), true)
		}
	} else {
		bb = MainBranch()
		pullMain()
	}

	sh("git checkout -b %s %s", br, bb)
	if bb != "" {
		revertEmacsBuffers()
	}
}

func (op OpList) BrTrackRemoteBranch() {
	parseFlag()
	bc := CurBranch()
	br := remoteBranch(bc)
	check.T(bc == br).F("remote branch mismatch", "current", bc, "remote", br)
	sh("git branch -u origin/%s", bc)
}

func (op OpList) BdDeleteBranchLocalAndRemote() {
	localOnly := flag.Bool("l", false, "local branch only")
	parseFlag("branch_re_or_dot")
	pat := flag.Arg(0)

	if pat == "." {
		deleteThisBranch()
		return
	}

	sh("git fetch --prune --tags")

	lbrs := matchLocalBranches(pat, false, true)
	var rbrs []string
	if !*localOnly {
		rbrs = matchRemoteBranches(pat, true, true)
	}
	if len(lbrs) == 0 && len(rbrs) == 0 {
		log.Printf("no branch found by %s", pat)
		return
	}
	if !strings.HasSuffix(pat, tmpSuffix) {
		yorn("delete local branches:%v and remote branches:%v", lbrs, rbrs)
	}
	deleteBranches(lbrs, rbrs)
}

func deleteBranches(lbrs, rbrs []string) {
	for _, br := range lbrs {
		sh("git branch -D %s", br)
	}
	for _, br := range rbrs {
		sh("git push origin :%s", br)
	}
}

func deleteThisBranch() {
	bc := CurBranch()
	br := RepoBranch()
	check.T(bc != br).F("cannot delete repo branch", "repo_branch", bc)

	rbrs := matchRemoteBranches(bc, true, true)
	yorn("delete this branch:%s and remote branches:%v", bc, rbrs)

	checkoutBranch(br, true)
	deleteBranches([]string{bc}, rbrs)
}

func yorn(s string, args ...any) {
	if len(args) > 0 {
		s = fmt.Sprintf(s, args...)
	}
	fmt.Print(s + " ([y]/n)?: ")
	b := make([]byte, 2)
	n := check.Vp(os.Stdin.Read(b))
	check.T(n <= 1 || b[0] == 'y').F("aborted")
}

func (op OpList) CaCherryPickAbort() {
	parseFlag()
	sh("git cherry-pick --abort")
}

func (op OpList) CcCherryPickContinue() {
	parseFlag()
	sh("git cherry-pick --continue")
}

func (op OpList) CpCherryPick() {
	parseFlag("commit_or_branch_re")
	cm := flag.Arg(0)

	if !isCommit(cm) {
		cm = localBranch(cm, true)
	}
	sh("git cherry-pick %s", cm)
}

func (op OpList) PmPullMain() {
	parseFlag()
	pullMain()
}

func (op OpList) PlPull() {
	parseFlag()
	sh("git pull --rebase")
}

func (op OpList) PsPush() {
	force := flag.Bool("f", false, "force push")
	parseFlag()
	bc := CurBranch()
	bm := MainBranch()
	if bc == bm {
		check.T(*force).F("cannot force push to main")
		s := sh("git log --oneline origin/%s..%s", bm, bm)
		lns := strings.Split(s, "\n")
		for _, ln := range lns {
			ln := strings.ToLower(strings.TrimSpace(ln))
			check.T(!strings.HasSuffix(ln, "wip") && !strings.Contains(ln, " wip ")).F("cannot push wip commit to main")
		}
	}
	if *force {
		sh("git push -f origin HEAD:%s", bc)
	} else {
		sh("git push origin HEAD:%s", bc)
	}
}

func (op OpList) PoSubmoduleUpdate() {
	sh("git submodule update --init")
}

func (op OpList) MwWip() {
	parseFlag()
	bc := CurBranch()
	br := RepoBranch()
	bm := MainBranch()
	check.T(bc != br && bc != bm).F("cannot wip on default branch", "main", bm, "repo", br)
	if isStaged() {
		sh("git commit -m wip")
	} else {
		sh("git commit -a -m wip")
	}
}

func isStaged() bool {
	s := shQ("git diff-index --cached HEAD")
	return strings.TrimSpace(s) != ""
}

func (op OpList) MrDiscardModified() {
	parseFlag("file...")
	s := quoteArgs(flag.Args(), "")
	matched := sh("git ls-files -m %s", s)
	yorn("discard modified: %s", strings.Replace(matched, "\n", " ", -1))
	sh("git checkout %s", s)
}

func (op OpList) MxClean() {
	parseFlag()
	s := sh("git ls-files --others --exclude-standard")
	s = strings.TrimSpace(s)
	if s == "" {
		log.Println("no file to clean")
		return
	}
	yorn("delete these files?\n%s\n", s)
	sh("git clean -f")
}

func (op OpList) McCommit() {
	force := flag.Bool("f", false, "force commit")
	parseFlag("commit_message...")
	bc := CurBranch()
	bm := MainBranch()
	br := RepoBranch()
	check.T(*force || (bc != bm && bc != br)).F("cannot commit to default branch", "main", bm, "repo", br)
	msg := quoteArgs(flag.Args(), "-m")
	if isStaged() {
		sh("git commit %s", msg)
	} else {
		sh("git commit -a %s", msg)
	}
}

func (op OpList) MaAddFiles() {
	parseFlag("file...")
	sh("git add %s", quoteArgs(flag.Args(), ""))
}

func quoteArgs(args []string, prefix string) string {
	var sb strings.Builder
	for i, arg := range args {
		if i > 0 {
			sb.WriteString(" ")
		}
		if prefix != "" {
			sb.WriteString(prefix)
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, `"%s"`, arg)
	}
	return sb.String()
}

func (op OpList) MmAmendLastCommit() {
	parseFlag("commit_message...")
	msg := quoteArgs(flag.Args(), "-m")
	sh("git commit --amend %s", msg)
}

func (op OpList) MhStash() {
	parseFlag()
	sh("git stash")
}

func (op OpList) MsPopStash() {
	parseFlag()
	sh("git stash pop")
}

func (op OpList) MuUnstage() {
	parseFlag("file...")
	sh("git restore --staged %s", quoteArgs(flag.Args(), ""))
}

func (op OpList) DfDiff() {
	cached := flag.Bool("c", false, "cached")
	parseFlag("[diff_arg]")
	args := diffArgs(*cached, "", flag.Args())
	fmt.Println(sh("git diff %s", args))
}

func diffArgs(cached bool, difftool string, args []string) string {
	var sb strings.Builder
	if cached {
		sb.WriteString("--cached ")
	}
	switch difftool {
	case "ediff":
		sb.WriteString("-t ediff ")
	case "difftool":
		if env := os.Getenv("MYGIT_DIFFTOOL"); env != "" {
			sb.WriteString("-t ")
			sb.WriteString(env)
			sb.WriteString(" ")
		}
	}
	sb.WriteString(quoteArgs(args, ""))
	return sb.String()
}

func (op OpList) DgGuiDiff() {
	cached := flag.Bool("c", false, "cached")
	parseFlag("[diff_arg]")
	sh("git difftool %s", diffArgs(*cached, "difftool", flag.Args()))

	shQ("tabfilemerge.sh")
}

func (op OpList) DeEdiff() {
	cached := flag.Bool("c", false, "cached")
	parseFlag("[diff_arg_or_^]")
	var args []string
	if flag.NArg() == 1 && flag.Arg(0) == "^" {
		args = []string{"HEAD~"}
	} else {
		args = flag.Args()
	}
	sh("git difftool %s", diffArgs(*cached, "ediff", args))
}

func (op OpList) DcGuiDiffCommit() {
	cached := flag.Bool("c", false, "cached")
	parseFlag("[commit]")
	cm := "HEAD"
	if flag.NArg() > 0 {
		cm = flag.Arg(0)
		if !isCommit(cm) {
			cm = localBranch(cm, true)
		}
	}
	args := []string{fmt.Sprintf("%s~..%s", cm, cm)}
	sh("git difftool %s", diffArgs(*cached, "difftool", args))
}

func (op OpList) RiRebaseInteractive() {
	parseFlag("branch_re")
	cm := flag.Arg(0)
	if !strings.Contains(cm, "~") && !strings.Contains(cm, "^") && !isCommit(cm) {
		cm = localBranch(cm, true)
	}
	sh("git rebase -i %s", cm)
	revertEmacsBuffers()
}

func (op OpList) RcRebaseCont() {
	parseFlag()
	sh("git add")
	sh("git rebase --continue")
}

func (op OpList) RaRebaseAbort() {
	sh("git rebase --abort")
}

func (op OpList) RrRebase() {
	parseFlag("[branch_re]")
	var br string
	if flag.NArg() == 0 {
		pullMain()
		br = RepoBranch()
	} else {
		br = localBranch(flag.Arg(0), true)
	}
	sh("git rebase %s", br)
	revertEmacsBuffers()
}

func (op OpList) RbRebaseBackOnto() {
	numCommits := flag.Int("n", 1, "number of commits to keep")
	revert := flag.Bool("r", false, "revert emacs buffers")
	parseFlag("[branch_re]")
	var onto string
	if flag.NArg() == 0 {
		onto = MainBranch()
		pullMain()
	} else {
		onto = localBranch(flag.Arg(0), true)
	}
	deleteTmpBranch()

	bc := CurBranch()
	bcTmp := bc + tmpSuffix
	sh("git branch %s HEAD~{%d}", bcTmp, *numCommits)
	sh("git rebase --onto %s %s %s", onto, bcTmp, bc)
	sh("git branch -D %s", bcTmp)
	if *revert {
		revertEmacsBuffers()
	}

	if prState("") == "OPEN" {
		resetGithubBase(onto)
		sh("git push -f origin HEAD:%s", bc)
	}
}

func deleteTmpBranch() {
	lbrs := matchLocalBranches(tmpSuffix, false, true)
	rbrs := matchRemoteBranches(tmpSuffix, true, true)
	deleteBranches(lbrs, rbrs)
}

func prState(pr string) string {
	return shQ("gh pr view %s --json state -q .state", pr)
}

func resetGithubBase(bb string) {
	rbb := CurBranch() + tmpSuffix
	bm := MainBranch()
	if bb != bm {
		sh("git push --force origin %s:%s", bb, rbb)
		sh("gh pr edit -B %s", rbb)
	} else if matchRemoteBranches(rbb, true, true) != nil {
		log.Printf("reset pr base to main")
		sh("gh pr edit -B %s", bm)
		sh("git push origin :%s", rbb)
	}
}

func (op OpList) RuUncommits() {
	uncommitDeleteOrSquash("uncommit")
}

func (op OpList) RdDeleteCommits() {
	uncommitDeleteOrSquash("delete")
}

func (op OpList) RsSquashToCommit() {
	uncommitDeleteOrSquash("squash")
}

func uncommitDeleteOrSquash(action string) {
	parseFlag("[n_commits_or_commit]")
	cm := parseNumCommitsOrCommit(action == "squash")
	start := sh("git rev-parse --short %s", cm)
	end := sh("git rev-parse --short %s", CurBranch())

	switch action {
	case "uncommit":
		yorn("undo commits [%s..%s]", start, end)
		sh("git reset --mixed %s~", cm)
	case "delete":
		yorn("delete commits [%s..%s]", start, end)
		sh("git reset --hard %s~", cm)
	case "squash":
		yorn("squash commits [%s..%s]", start, end)
		msg := sh("git show -s --format=%B " + cm) // cannot format because of %B
		sh("git reset --soft %s~", cm)
		sh(`git commit -m '%s'`, msg)
	default:
		panic(action)
	}
}

func parseNumCommitsOrCommit(squash bool) string {
	if flag.NArg() == 0 {
		if squash {
			return "HEAD~1"
		}
		return "HEAD"
	}
	n, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		return flag.Arg(0)
	}
	check.T(n > 0 && n <= 9 && (n != 1 || !squash)).F("invalid n_commits", "n", n)
	if n == 1 {
		return "HEAD"
	}
	return fmt.Sprintf("HEAD~%d", n-1)
}

func (op OpList) RtResetToCommit() {
	parseFlag("[branch_re]")
	var br string
	if flag.NArg() > 0 {
		br = localBranch(flag.Arg(0), true)
	} else {
		br = MainBranch()
	}
	yorn("reset %s to %s", CurBranch(), br)
	sh("git reset --hard %s --", br)
}

var prInTitleRe = regexp.MustCompile(`\(#[0-9]+\)$`)

func (op OpList) SShowStatusLocalBranches() {
	parseFlag()

	var sb strings.Builder
	sep := "================"
	s := sh("git status -b")
	for i, ln := range strings.Split(s, "\n") {
		if i == 0 {
			ln = strings.TrimPrefix(ln, "On branch ")
			s = sh("git log -1 --oneline --no-decorate")
			sb.WriteString(ln)
			sb.WriteByte('\t')
			sb.WriteString(s)
			sb.WriteByte('\n')
			sb.WriteString(sep)
		} else {
			sb.WriteString(ln)
		}
		sb.WriteByte('\n')
	}

	if sh("git status --porcelain") == "" {
		title := sh("git log -n 1 --format=%s")
		if !prInTitleRe.MatchString(title) {
			s = sh(`git log -n 1 --format="" --name-only`)
			for _, f := range strings.Split(s, "\n") {
				sb.WriteString("   - ")
				sb.WriteString(f)
				sb.WriteByte('\n')
			}
		}
	}
	sb.WriteString(sep)
	sb.WriteString("\n  ")
	sb.WriteString(sh("git branch -v"))
	fmt.Print(&sb)
}

func (op OpList) ScShowCommitSummary() {
	parseFlag("[commit]")
	s := sh("git show --name-only %s", quoteArgs(flag.Args(), ""))
	fmt.Println(s)
}

func (op OpList) SlListCommits() {
	remote := flag.Bool("r", false, "remote branch")
	parseFlag("[branch_re]", "[n_commits]")

	var pat string
	num := 3
	for _, s := range flag.Args() {
		n, err := strconv.Atoi(s)
		if err != nil {
			pat = s
		} else {
			num = n
		}
	}

	var br string
	if pat != "" {
		if *remote {
			br = remoteBranch(pat)
		} else {
			br = localBranch(pat, true)
		}
	}

	logFormat := "--format='%h    %s%n%cd    %an%n' --date=local"
	s := sh("git log -n %d %s %s --", num, logFormat, br)
	fmt.Println(s)
}

func (op OpList) SrListRemoteBranches() {
	parseFlag("[branch_re]")

	pat := ".*"
	if flag.NArg() > 0 {
		pat = flag.Arg(0)
	}
	rbrs := matchRemoteBranches(pat, false, false)
	for _, br := range rbrs {
		fmt.Println(br)
	}
}

func (op OpList) SvShowFileAtVersion() {
	parseFlag("branch_re_or_commit", "file")
	cm, filename := flag.Arg(0), flag.Arg(1)
	if _, err := os.Stat(filename); err != nil {
		cm, filename = filename, cm
	}
	check.V(os.Stat(filename)).F("not file", "arg0", cm, "arg1", filename)
	if !isCommit(cm) {
		cm = localBranch(cm, true)
	}

	filename, _ = filepath.Abs(filename)
	rd := filepath.Clean(RepoDir())
	fn := strings.TrimPrefix(filename, rd)
	check.T(fn != filename).F("filename not in repo", "filename", filename, "repo", rd)
	filename = strings.TrimLeft(fn, "/")

	s := sh(`git show %s:"%s"`, cm, filename)
	fmt.Println(s)
}

func (op OpList) GhGithubPrStatus() {
	parseFlag()
	s := shQ("gh pr status")
	fmt.Println(s)
}

func (op OpList) GtGithubPrDraft() {
	draft := flag.Bool("w", false, "draft pr")
	silent := flag.Bool("s", false, "don't open browser")
	parseFlag("[branch_re_or_commit]")
	var bb string
	if flag.NArg() > 0 {
		bb = flag.Arg(0)
		if !isCommit(bb) {
			bb = localBranch(bb, true)
		}
	}
	var draftArg string
	if *draft {
		draftArg = "--draft"
	}

	bc := CurBranch()
	sh("git push --force origin HEAD:%s", bc)

	if bb == "" {
		sh("gh pr create %s --fill", draftArg)
	} else {
		rbb := bc + tmpSuffix
		sh("git push --force origin %s:%s", bb, rbb)
		sh("gh pr create %s --fill -B %s", draftArg, rbb)
	}

	if !*silent {
		showPR(bc)
	}
}

func showPR(br string) {
	state := prState(br)
	check.T(state == "OPEN").F("no pr is open", "branch", br)

	url := shQ("gh pr view %s --json url -q .url", br)
	check.T(url != "").F("not pr url found", "branch", br)
	shQ(`open "%s"`, url)
}

func (op OpList) GpGithubThisPullrequest() {
	parseFlag("[branch_re_or_pr]")
	var br string
	if flag.NArg() > 0 {
		br = flag.Arg(0)
		lbrs := matchLocalBranches(br, true, false)
		if len(lbrs) == 1 {
			br = lbrs[0]
		}
	}
	showPR(br)
}

func (op OpList) GsGithubStatus() {
	parseFlag("[branch_re_or_dot]")
	if flag.NArg() == 0 {
		fmt.Println("gh pr status")
		return
	}

	bc, bm, rb := CurBranch(), MainBranch(), RepoBranch()

	br := flag.Arg(0)
	if br == "." {
		br = bc
	} else {
		br = localBranch(br, false)
	}

	state := prState(br)
	if state == "MERGED" {
		switch br {
		case bm, rb:
			fmt.Println(state)
		case bc:
			yorn("reset to %s", bm)
			sh("git reset --hard %s --", bm)
		default:
			lbrs := matchLocalBranches(br, false, true)
			rbrs := matchRemoteBranches(br, true, true)
			yorn("delete local branches:%v and remote branches:%v", lbrs, rbrs)
			deleteBranches(lbrs, rbrs)
		}
	}
}

func (op OpList) WnWorktreeAdd() {
	parseFlag("worktree_id")
	wt := flag.Arg(0)
	check.T(!strings.HasPrefix(wt, "wt-")).F("worktree_id cannot begin with wt-")

	bw := fmt.Sprintf("wt-%s", wt)
	rd := RepoDir()
	wd := filepath.Join(filepath.Dir(rd), bw)
	sh(`git worktree add -b %s "%s"`, bw, wd)
	log.Printf("worktree %q is created at %q", bw, wd)
}

func (op OpList) WlWorktreeList() {
	fmt.Println(sh("git worktree list"))
}

func (op OpList) WdWorktreeRemove() {
	parseFlag("worktree_id")
	wt := flag.Arg(0)

	bw := fmt.Sprintf("wt-%s", wt)
	rd := RepoDir()
	wd := filepath.Join(filepath.Dir(rd), bw)
	sh("git worktree remove %s", bw)
	sh("git branch -D %s", bw)
	log.Printf("worktree %q at %q is deleted", bw, wd)
}

func (op OpList) IHead() {
	parseFlag()
	br := shQ("git rev-parse --abbrev-ref HEAD")
	if br == "" {
		return
	}
	if br == "HEAD" {
		br = sh("git rev-parse --short HEAD")
	}
	fmt.Print(br)
}

func (op OpList) RepoShowRepoName() {
	parseFlag()
	fmt.Print(filepath.Base(RepoDir()))
}

func (op OpList) createOps() {
	cleanOnly := flag.Bool("c", false, "clean only")
	parseFlag("prefix")
	prefix := flag.Arg(0)

	binDir, binName := filepath.Split(progName)
	os.Chdir(binDir)

	cmds, _ := filepath.Glob(fmt.Sprintf("%s.*", prefix))
	for _, c := range cmds {
		if st, err := os.Lstat(c); err == nil && st.Mode()&os.ModeSymlink != 0 {
			os.Remove(c)
		}
	}
	if *cleanOnly {
		return
	}

	for _, op := range gitops {
		name := fmt.Sprintf("%s.%s", prefix, op.Alias)
		log.Println("create", name)
		os.Symlink(binName, name)
	}
}

func (op OpList) exitWithUsage() {
	var aliases []string
	for _, op := range gitops {
		aliases = append(aliases, op.Alias)
	}
	slices.Sort(aliases)

	for _, alias := range aliases {
		fmt.Printf("%s => %s\n", alias, gitops[alias].Name)
	}
}

type GitOp struct {
	Alias string
	Name  string
	Func  func(OpList)
}

func (op *GitOp) String() string {
	return fmt.Sprintf("%s => %s", op.Alias, op.Name)
}

var gitops = make(map[string]*GitOp)

func buildGitOps() {
	rt := reflect.TypeOf(OpList{})
	for i := 0; i < rt.NumMethod(); i++ {
		alias, name, fn := buildMethod(rt.Method(i))
		_, ok := gitops[alias]
		check.T(!ok).F("alias in use", "alias", alias)
		op := &GitOp{Alias: alias, Name: name, Func: fn}
		gitops[alias] = op
		// slog.Info("register", "op", op)
	}

	gitops["create"] = &GitOp{Alias: "create", Name: "create ops", Func: OpList.createOps}
	gitops["help"] = &GitOp{Alias: "help", Name: "help ops", Func: OpList.exitWithUsage}
}

var nameRe = regexp.MustCompile(`[A-Z][a-z]*`)

func buildMethod(m reflect.Method) (alias, name string, fn func(OpList)) {
	mo := nameRe.FindAllString(m.Name, -1)
	check.T(mo != nil).F("invalid op method", "name", m.Name)
	alias = strings.ToLower(mo[0])
	var nn []string
	for _, s := range mo[1:] {
		nn = append(nn, strings.ToLower(s))
	}
	name = strings.Join(nn, " ")
	fn = m.Func.Interface().(func(OpList))
	return alias, name, fn
}

func stripCmdPrefix(s string) string {
	i := strings.Index(s, ".")
	if i < 0 {
		return s
	}
	return s[i+1:]
}

func absProgName() string {
	p, err := exec.LookPath(os.Args[0])
	if err != nil {
		check.T(errors.Is(err, exec.ErrDot)).F("lookpath", "err", err, "args0", os.Args[0])
	}
	return check.Vp(filepath.Abs(p))
}

var progName = absProgName()

func parseOp() string {
	cmd := filepath.Base(os.Args[0])
	if cmd == "mygit" {
		os.Args = os.Args[1:]
		return os.Args[0]
	} else {
		return stripCmdPrefix(cmd)
	}
}

func parseFlag(args ...string) {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s %s\n", os.Args[0], strings.Join(args, " "))
		flag.PrintDefaults()
	}
	flag.Parse()

	var n int
	var opt bool
	for _, arg := range args {
		if strings.HasPrefix(arg, "[") {
			opt = true
		} else {
			check.T(!opt).F("required arg appears after optional arg", "args", args)
			n++
		}
	}
	check.T(n <= flag.NArg()).F("missing required arg", "args", args)
}

func main() {
	// mygit op arg...
	// when op alias is defined, <prefix>.<alias>arg...
	buildGitOps()

	log.SetFlags(0)
	log.SetPrefix("# ")

	alias := parseOp()
	op, ok := gitops[alias]
	if !ok {
		op = gitops["help"]
	}
	op.Func(OpList{})
}
