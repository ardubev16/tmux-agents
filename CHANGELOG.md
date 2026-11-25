# Changelog

## 0.1.0 (2026-11-25)


### ⚠ BREAKING CHANGES

* the dashboard comes from GitHub releases; until the first release is published, it cannot start. Go is no longer needed.
* **notify:** without notify-send, the default 'system' mode no longer shows anything; set @tmux_agents_notify 'tmux' for status-line messages.
* rewrite the dashboard in Go
* drop herdr, track agents natively in tmux as tmux-agents

### Features

* add agent_dashboard and agent attach scripts ([5f1600d](https://github.com/ardubev16/tmux-agents/commit/5f1600dd09136245da3a87de89918f7a03619ff2))
* add claude.md and convert tests to bats ([0bb9e6d](https://github.com/ardubev16/tmux-agents/commit/0bb9e6d7bebabfdd2179d1da80e558c57a968390))
* add debug mode ([5ea9177](https://github.com/ardubev16/tmux-agents/commit/5ea91775a4055a419a40e228ae907ca3585fe982))
* add distinction between branch and default agent spawn ([9251148](https://github.com/ardubev16/tmux-agents/commit/925114805ef794a39086c11c977a7c252838cfbe))
* add install/uninstall script for harness integrations ([1be3d4a](https://github.com/ardubev16/tmux-agents/commit/1be3d4ac92b0a5b43c5fbc17aeb4c217bcc9b7fe))
* add new_agent command ([73ca805](https://github.com/ardubev16/tmux-agents/commit/73ca80573c3c0a6522993ef8cb7409fb53f6689d))
* add status bar support ([dbd8716](https://github.com/ardubev16/tmux-agents/commit/dbd871638a00ca2956a527bd8cef7e9ad53aac4f))
* add the tmux_agents Python package for the dashboard ([7349ad0](https://github.com/ardubev16/tmux-agents/commit/7349ad0e4405010d01286ff4978ae6198f272126))
* cleanup vars, add support for path in git ([cb23512](https://github.com/ardubev16/tmux-agents/commit/cb23512a5d3704d26738b7033a14a0c69bc471f3))
* configurable harness ([ffd762d](https://github.com/ardubev16/tmux-agents/commit/ffd762da2164beba2435630419fc4ee0956687e5))
* **dashboard:** [@tmux](https://github.com/tmux)_agents_dashboard_build to build from the checkout instead of downloading ([88c15e7](https://github.com/ardubev16/tmux-agents/commit/88c15e7d028587ad6ee80fd5195c4c3f48f5bfb5))
* **dashboard:** forward the mouse to lazygit, show unpushed commits as ⇡N ([e627b2d](https://github.com/ardubev16/tmux-agents/commit/e627b2d8d23955cf50901394eda7f2b19a849b57))
* **dashboard:** lazygit beside the list, fuzzy filter, unpushed commits, running-only timer ([9ccc2d3](https://github.com/ardubev16/tmux-agents/commit/9ccc2d37300204e7006a21651fc1099f6b7ed7e5))
* **dashboard:** sections, highlighted selection, chat header and scrollbar, omp markdown ([65625f6](https://github.com/ardubev16/tmux-agents/commit/65625f6b016dcc96f1abf35e7433db26ea6e0e53))
* **dashboard:** session names, unread/needs-input marks, close agent ([0abee79](https://github.com/ardubev16/tmux-agents/commit/0abee798d6984b4fa7625583fd8c2ec9ad8ffc54))
* **dashboard:** terminal colors, queued prompts, q to close ([ed176a8](https://github.com/ardubev16/tmux-agents/commit/ed176a8a80eafdbdf496735d53ea9049ebd2e286))
* desktop notifications and graceful agent close ([1823727](https://github.com/ardubev16/tmux-agents/commit/1823727233b8e268499ff14122cf560ce5516f58))
* download the released dashboard instead of building it with Go ([3e5109b](https://github.com/ardubev16/tmux-agents/commit/3e5109b8e34343fbeeb8780fa28bb3d210f68e0b))
* drop herdr, track agents natively in tmux as tmux-agents ([13495d4](https://github.com/ardubev16/tmux-agents/commit/13495d4f5fc69ad95f972c37023cea223b9e04f9))
* **hooks:** Claude Code hooks call main.sh claude_hook, questions mark the agent waiting, macOS notifications ([64f0e36](https://github.com/ardubev16/tmux-agents/commit/64f0e36ca2901535b5374b79a967e5f43db61c05))
* init herdr with correct config ([d1b68a7](https://github.com/ardubev16/tmux-agents/commit/d1b68a7081aefdd5e27357e11edd5936789ce5df))
* **notify:** drop the status-line fallback for desktop notifications ([c838699](https://github.com/ardubev16/tmux-agents/commit/c838699bc354b87e5e9ab406b4bd82e341a3f562))
* override herdr config ([eaf35bb](https://github.com/ardubev16/tmux-agents/commit/eaf35bbc92230747968269550354f4457e4c7a19))
* render agent chats natively in the dashboard ([5bcc5b5](https://github.com/ardubev16/tmux-agents/commit/5bcc5b57ebc0d30ca7ffc19f76289a6cd3b104fe))
* replace fzf dashboard with a Textual TUI ([ac23cbe](https://github.com/ardubev16/tmux-agents/commit/ac23cbee729cfa16d717c3d7ae41868a70f64755))
* rewrite the dashboard in Go ([0d25ee3](https://github.com/ardubev16/tmux-agents/commit/0d25ee3fb5424c329be63f344814c330f5291343))
* split new agent command into two, add options for plugin showtcuts ([8579ee0](https://github.com/ardubev16/tmux-agents/commit/8579ee02e04db3baabeea5c9f84e254cfc088bab))
* **status:** hide zero counters by default ([551a043](https://github.com/ardubev16/tmux-agents/commit/551a0438ac0502aaba8d59f230cf2d2b83c84d67))
* time in state, hide-zero status, worktree removal, diff summary, lazygit, filter, question previews ([c1be243](https://github.com/ardubev16/tmux-agents/commit/c1be243d6f992d26d2b212b36a9b9308afc55e92))
* use defined standard project structure ([37b4f65](https://github.com/ardubev16/tmux-agents/commit/37b4f65cfc0ed44e05d06cadd3921f3b52906b6b))
* wrapped live chat and shared prompt drafts in the dashboard ([08c97ad](https://github.com/ardubev16/tmux-agents/commit/08c97ad0d1c117dcaadeac976f51a91629526cec))


### Bug Fixes

* attached agent return 1 after closing ([fd9c1a9](https://github.com/ardubev16/tmux-agents/commit/fd9c1a91f4a41df6948e86ef3cd6d8b57552ffa0))
* audit findings (reply target, worktree quoting, dashboard memory and IO) ([d040ae5](https://github.com/ardubev16/tmux-agents/commit/d040ae551952fccfc7223efe567c0f12bd18f78b))
* branch selector use correct path ([9bd9613](https://github.com/ardubev16/tmux-agents/commit/9bd9613508e1db7a288bc7f2b4a8f07927125fc2))
* compatibility issue with jq 1.6 ([404a718](https://github.com/ardubev16/tmux-agents/commit/404a718a13afa0900f7db50acd25f865580df509))
* **dashboard:** no silent fallbacks for omp replies and untitled agent names ([32b5129](https://github.com/ardubev16/tmux-agents/commit/32b51297390a7830b8571f0f2c55b9a4147adc15))
* first agent correctly attaches on first try ([44e334f](https://github.com/ardubev16/tmux-agents/commit/44e334fb764a44d38dca58c01f8c8752ba1ebdf2))
* resolve TMUX_AGENTS through tmux in panes that predate the plugin ([ad37184](https://github.com/ardubev16/tmux-agents/commit/ad3718490cb14c9eebc19632a02466d711d5ae98))


### Performance Improvements

* **dashboard:** close instantly ([dbf4c38](https://github.com/ardubev16/tmux-agents/commit/dbf4c383b22511e7de417be6fcc696e945df3c76))
* **dashboard:** read session files from the end, load older messages on scroll ([e93b536](https://github.com/ardubev16/tmux-agents/commit/e93b53623338c795c67840a49ab0454bd799bec5))
* **dashboard:** refresh the agent list and git summary in the background ([814dc16](https://github.com/ardubev16/tmux-agents/commit/814dc165da4c2e82e25c0c531f6b4dfe4d72d3be))
