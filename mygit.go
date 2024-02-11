package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/zncoder/check"
	"github.com/zncoder/mygo"
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

	c := mygo.NewCmd("/bin/sh", "-c", s).IgnoreErr(ignoreErr)
	return string(bytes.TrimSpace(c.Stdout()))
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
		u := check.V(user.Current()).P()
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
	wd := check.V(os.Getwd()).P()
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
		log.Printf("switch to repo branch: %s", br)
		checkoutBranch(br, false)
	}
	log.Printf("pull in %s", br)
	sh("git rebase %s", bm)

	if bc != bm || bc != br {
		checkoutBranch(bc, false)
	}
}

func (OpList) BO_CheckoutLocalBranch() {
	revertBuf := flag.Bool("r", false, "revert emacs buffers")
	mygo.ParseFlag("[branch_re]")

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

func (OpList) BC_CheckoutCommit() {
	mygo.ParseFlag("commit_or_tag")
	cot := flag.Arg(0)
	if isCommit(cot) {
		sh("git checkout --detach %s", cot)
	} else {
		sh("git checkout tags/%s -b %s", cot, cot)
	}
}

func (OpList) BT_CheckoutAndTrackRemoteBranch() {
	mygo.ParseFlag("remote_branch_re")
	br := remoteBranch(flag.Arg(0))
	sh("git checkout -b %s --track origin/%s", br, br)
}

func (OpList) BN_NewBranch() {
	mygo.ParseFlag("branch_name", "[base_branch_re_or_dot]")

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

func (OpList) BM_RenameBranch() {
	mygo.ParseFlag("branch_name")
	br := flag.Arg(0)
	check.T(!strings.Contains(br, "/")).F("branch name cannot contain `/`", "branch", br)
	br = fmt.Sprintf("%s/%s", Username(), br)

	bc, bm, rb := CurBranch(), MainBranch(), RepoBranch()
	check.T(bc != bm && bc != rb).F("cannot rename main or repo branch")

	ps := prState(bc)
	check.T(ps == "MERGED").F("cannot rename branch with open pr", "state", ps, "branch", bc)
	pullMain()
	log.Printf("create branch:%s", br)
	sh("git checkout -b %s %s", br, bm)
	cm := sh("git rev-parse --short %s", bc)
	log.Printf("delete branch:%s [%s]", bc, cm)
	deleteBranches([]string{bc}, []string{})
}

func (OpList) BR_TrackRemoteBranch() {
	mygo.ParseFlag()
	bc := CurBranch()
	br := remoteBranch(bc)
	check.T(bc == br).F("remote branch mismatch", "current", bc, "remote", br)
	sh("git branch -u origin/%s", bc)
}

func (OpList) BD_DeleteBranchLocalAndRemote() {
	localOnly := flag.Bool("l", false, "local branch only")
	mygo.ParseFlag("branch_re_or_dot")
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
	n := check.V(os.Stdin.Read(b)).P()
	check.T(n <= 1 || b[0] == 'y').F("aborted")
}

func (OpList) CA_CherryPickAbort() {
	mygo.ParseFlag()
	sh("git cherry-pick --abort")
}

func (OpList) CC_CherryPickContinue() {
	mygo.ParseFlag()
	sh("git cherry-pick --continue")
}

func (OpList) CP_CherryPick() {
	mygo.ParseFlag("commit_or_branch_re")
	cm := flag.Arg(0)

	if !isCommit(cm) {
		cm = localBranch(cm, true)
	}
	sh("git cherry-pick %s", cm)
}

func (OpList) PM_PullMain() {
	mygo.ParseFlag()
	pullMain()
}

func (OpList) PL_Pull() {
	mygo.ParseFlag()
	sh("git pull --rebase")
}

func (OpList) PS_Push() {
	force := flag.Bool("f", false, "force push")
	mygo.ParseFlag()
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

func (OpList) PO_SubmoduleUpdate() {
	sh("git submodule update --init")
}

func (OpList) MW_Wip() {
	mygo.ParseFlag()
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

func (OpList) MR_DiscardModified() {
	mygo.ParseFlag("file...")
	s := quoteArgs(flag.Args(), "")
	matched := sh("git ls-files -m %s", s)
	yorn("discard modified: %s", strings.Replace(matched, "\n", " ", -1))
	sh("git checkout %s", s)
}

func (OpList) MX_Clean() {
	mygo.ParseFlag()
	s := sh("git ls-files --others --exclude-standard")
	s = strings.TrimSpace(s)
	if s == "" {
		log.Println("no file to clean")
		return
	}
	yorn("delete these files?\n%s\n", s)
	sh("git clean -f")
}

func (OpList) MC_Commit() {
	force := flag.Bool("f", false, "force commit")
	mygo.ParseFlag("commit_message...")
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

func (OpList) MA_AddFiles() {
	mygo.ParseFlag("file...")
	sh("git add %s", quoteArgs(flag.Args(), ""))
}

func (OpList) MP_ChoosePatch() {
	mygo.ParseFlag()
	mygo.NewCmd("git", "add", "-p").Interactive()
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

func (OpList) MM_AmendLastCommit() {
	mygo.ParseFlag("commit_message...")
	msg := quoteArgs(flag.Args(), "-m")
	sh("git commit --amend %s", msg)
}

func (OpList) MH_Stash() {
	mygo.ParseFlag()
	sh("git stash")
}

func (OpList) MS_PopStash() {
	mygo.ParseFlag()
	sh("git stash pop")
}

func (OpList) MU_Unstage() {
	mygo.ParseFlag("file...")
	sh("git restore --staged %s", quoteArgs(flag.Args(), ""))
}

func (OpList) DF_Diff() {
	cached := flag.Bool("c", false, "cached")
	mygo.ParseFlag("[diff_arg]")
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

func (OpList) DG_GuiDiff() {
	cached := flag.Bool("c", false, "cached")
	mygo.ParseFlag("[diff_arg]")
	sh("git difftool %s", diffArgs(*cached, "difftool", flag.Args()))

	shQ("tabfilemerge.sh")
}

func (OpList) DE_Ediff() {
	cached := flag.Bool("c", false, "cached")
	mygo.ParseFlag("[diff_arg_or_^]")
	var args []string
	if flag.NArg() == 1 && flag.Arg(0) == "^" {
		args = []string{"HEAD~"}
	} else {
		args = flag.Args()
	}
	sh("git difftool %s", diffArgs(*cached, "ediff", args))
}

func (OpList) DC_GuiDiffCommit() {
	cached := flag.Bool("c", false, "cached")
	mygo.ParseFlag("[commit]")
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

func (OpList) RI_RebaseInteractive() {
	mygo.ParseFlag("branch_re")
	cm := flag.Arg(0)
	if !strings.Contains(cm, "~") && !strings.Contains(cm, "^") && !isCommit(cm) {
		cm = localBranch(cm, true)
	}
	sh("git rebase -i %s", cm)
	revertEmacsBuffers()
}

func (OpList) RC_RebaseCont() {
	mygo.ParseFlag()
	sh("git add")
	sh("git rebase --continue")
}

func (OpList) RA_RebaseAbort() {
	sh("git rebase --abort")
}

func (OpList) RR_Rebase() {
	mygo.ParseFlag("[branch_re]")
	bc := CurBranch()
	var br string
	if flag.NArg() == 0 {
		pullMain()
		br = RepoBranch()
	} else {
		br = localBranch(flag.Arg(0), true)
	}
	if bc != br {
		sh("git rebase %s", br)
	}
	revertEmacsBuffers()
}

func (OpList) RB_RebaseBackOnto() {
	numCommits := flag.Int("n", 1, "number of commits to keep")
	revert := flag.Bool("r", false, "revert emacs buffers")
	mygo.ParseFlag("[branch_re]")
	var onto string
	if flag.NArg() == 0 {
		onto = MainBranch()
		pullMain()
	} else {
		onto = localBranch(flag.Arg(0), true)
	}

	deleteTmpBranch(false)
	bc := CurBranch()
	bcTmp := bc + tmpSuffix
	sh("git branch %s HEAD~%d", bcTmp, *numCommits)
	sh("git rebase --onto %s %s %s", onto, bcTmp, bc)
	sh("git branch -D %s", bcTmp)
	if *revert {
		revertEmacsBuffers()
	}

	if prState("") == "OPEN" {
		resetGithubBase(onto)
		sh("git push -f origin HEAD:%s", bc)
	}

	deleteTmpBranch(true)
}

func deleteTmpBranch(remote bool) {
	lbrs := matchLocalBranches(tmpSuffix, false, true)
	var rbrs []string
	if remote {
		rbrs = matchRemoteBranches(tmpSuffix, true, true)
	}
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

func (OpList) RU_Uncommits() {
	uncommitDeleteOrSquash("uncommit")
}

func (OpList) RD_DeleteCommits() {
	uncommitDeleteOrSquash("delete")
}

func (OpList) RS_SquashToCommit() {
	uncommitDeleteOrSquash("squash")
}

func uncommitDeleteOrSquash(action string) {
	mygo.ParseFlag("[n_commits_or_commit]")
	cm, one := parseNumCommitsOrCommit(action == "squash")
	start := sh("git rev-parse --short %s", cm)
	end := sh("git rev-parse --short %s", CurBranch())

	switch action {
	case "uncommit":
		if one {
			log.Printf("undo commits [%s..%s]", start, end)
		} else {
			yorn("undo commits [%s..%s]", start, end)
		}
		sh("git reset --mixed %s~", cm)
	case "delete":
		yorn("delete commits [%s..%s]", start, end)
		sh("git reset --hard %s~", cm)
	case "squash":
		if one {
			log.Printf("squash commits [%s..%s]", start, end)
		} else {
			yorn("squash commits [%s..%s]", start, end)
		}
		msg := sh("git show -s --format=%B " + cm) // cannot format because of %B
		sh("git reset --soft %s~", cm)
		sh(`git commit -m '%s'`, msg)
	default:
		panic(action)
	}
}

func parseNumCommitsOrCommit(squash bool) (string, bool) {
	if flag.NArg() == 0 {
		if squash {
			return "HEAD~1", true
		}
		return "HEAD", true
	}
	n, err := strconv.Atoi(flag.Arg(0))
	if err != nil {
		return flag.Arg(0), false
	}
	check.T(n > 0 && n <= 9 && (n != 1 || !squash)).F("invalid n_commits", "n", n)
	if n == 1 {
		return "HEAD", true
	}
	return fmt.Sprintf("HEAD~%d", n-1), false
}

func (OpList) RT_ResetToCommit() {
	mygo.ParseFlag("[branch_re]")
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

func (OpList) S_ShowStatusLocalBranches() {
	mygo.ParseFlag()

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

func (OpList) SC_ShowCommitSummary() {
	mygo.ParseFlag("[commit]")
	s := sh("git show --name-only %s", quoteArgs(flag.Args(), ""))
	fmt.Println(s)
}

func (OpList) SL_ListCommits() {
	remote := flag.Bool("r", false, "remote branch")
	mygo.ParseFlag("[branch_re]", "[n_commits]")

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

func (OpList) SR_ListRemoteBranches() {
	mygo.ParseFlag("[branch_re]")

	pat := ".*"
	if flag.NArg() > 0 {
		pat = flag.Arg(0)
	}
	rbrs := matchRemoteBranches(pat, false, false)
	for _, br := range rbrs {
		fmt.Println(br)
	}
}

func (OpList) SV_ShowFileAtVersion() {
	mygo.ParseFlag("branch_re_or_commit", "file")
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

func (OpList) GH_GithubPrStatus() {
	mygo.ParseFlag()
	s := shQ("gh pr status")
	fmt.Println(s)
}

func (OpList) GT_GithubPrDraft() {
	draft := flag.Bool("w", false, "draft pr")
	silent := flag.Bool("s", false, "don't open browser")
	mygo.ParseFlag("[branch_re_or_commit]")
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

func (OpList) GP_GithubThisPullrequest() {
	mygo.ParseFlag("[branch_re_or_pr]")
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

func (OpList) GS_GithubStatus() {
	mygo.ParseFlag("[branch_re_or_dot]")
	if flag.NArg() == 0 {
		fmt.Println(sh("gh pr status"))
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
	if state == "MERGED" || state == "" {
		switch br {
		case bm, rb:
			fmt.Println(state)
		case bc:
			pullMain()
			yorn("reset to %s", bm)
			sh("git reset --hard %s --", bm)
		default:
			rbrs := matchRemoteBranches("^"+br+"$", true, true)
			yorn("delete local branch:%s and remote branches:%v", br, rbrs)
			deleteBranches([]string{br}, rbrs)
		}
	}
}

func (OpList) WN_WorktreeAdd() {
	mygo.ParseFlag("worktree_id")
	wt := flag.Arg(0)
	check.T(!strings.HasPrefix(wt, "wt-")).F("worktree_id cannot begin with wt-")

	bw := fmt.Sprintf("wt-%s", wt)
	rd := RepoDir()
	wd := filepath.Join(filepath.Dir(rd), bw)
	sh(`git worktree add -b %s "%s"`, bw, wd)
	log.Printf("worktree %q is created at %q", bw, wd)
}

func (OpList) WL_WorktreeList() {
	fmt.Println(sh("git worktree list"))
}

func (OpList) WD_WorktreeRemove() {
	mygo.ParseFlag("worktree_id")
	wt := flag.Arg(0)

	bw := fmt.Sprintf("wt-%s", wt)
	rd := RepoDir()
	wd := filepath.Join(filepath.Dir(rd), bw)
	sh("git worktree remove %s", bw)
	sh("git branch -D %s", bw)
	log.Printf("worktree %q at %q is deleted", bw, wd)
}

func (OpList) I_Head() {
	mygo.ParseFlag()
	br := shQ("git rev-parse --abbrev-ref HEAD")
	if br == "" {
		return
	}
	if br == "HEAD" {
		br = shQ("git rev-parse --short HEAD")
	}
	fmt.Print(br)
}

func (OpList) REPO_ShowRepoName() {
	mygo.ParseFlag()
	fmt.Print(filepath.Base(RepoDir()))
}

var gitops mygo.OPMap

func main() {
	log.SetFlags(0)
	log.SetPrefix("# ")

	gitops = mygo.BuildOPMap[OpList]()
	gitops.RunCmd()
}
