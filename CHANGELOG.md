# Changelog

## [0.2.0](https://github.com/ardubev16/tmux-agents/compare/v0.1.0...v0.2.0) (2026-11-28)


### ⚠ BREAKING CHANGES

* **dashboard:** lazygit opens with g instead of tab.
* **integrations:** install.sh needs the harnesses to act on, e.g. `install.sh install omp claude`.

### Features

* agents report what they are doing and need from you; the context card shows it with the branch's PR and CI ([ced1d11](https://github.com/ardubev16/tmux-agents/commit/ced1d117829d47ae616f1fffa33f7ac5e63deff0))
* **dashboard:** [@tmux](https://github.com/tmux)_agents_theme: terminal (default) or omp's gruvbox-dark ([97cbf4f](https://github.com/ardubev16/tmux-agents/commit/97cbf4f35b2d36b98e6cd931eae2a332ba417c96))
* **dashboard:** → shows the context card in full; drop its commits and files ([6ef50e8](https://github.com/ardubev16/tmux-agents/commit/6ef50e8223d3456a20dc26af7162f359c48cd4a0))
* **dashboard:** box prompts in a theme shade, expand the step being written, theme colors for steps ([fe4c45a](https://github.com/ardubev16/tmux-agents/commit/fe4c45aee4da7a4fcdc09d9a8a0bd44d24d35035))
* **dashboard:** browse the ? keybindings with j/k and read the highlighted one's detail ([8973550](https://github.com/ardubev16/tmux-agents/commit/89735505352bd2cdde1a0f7298032fc87e86be57))
* **dashboard:** color agents by project and show how long they have been idle ([c3adacf](https://github.com/ardubev16/tmux-agents/commit/c3adacf0460375300619011ae3c2aaccfabc59e7))
* **dashboard:** context card below the agent list, with your own note on the agent ([2e955bc](https://github.com/ardubev16/tmux-agents/commit/2e955bcb477576630abd6c86627fb09f6dc8b982))
* **dashboard:** count prompts sent while the agent worked toward its task; runtime at the right ([432e2bd](https://github.com/ardubev16/tmux-agents/commit/432e2bd774be72dc540b4141da66217a518e884b))
* **dashboard:** git summary at the header's right edge, tool calls muted ([c7a8d96](https://github.com/ardubev16/tmux-agents/commit/c7a8d96fed0b5e61767c04363cfc48478ee2286a))
* **dashboard:** highlight the agent of the pane the dashboard opens over ([333cbef](https://github.com/ardubev16/tmux-agents/commit/333cbef422e9dd044c8cba1cbcff5c401b62dacf))
* **dashboard:** keep each project's color for good and highlight agents with 1-9 ([dc9df65](https://github.com/ardubev16/tmux-agents/commit/dc9df65f82db11140190743e6cb6c9e6eb4b040f))
* **dashboard:** list the keybindings under ? and trim the bottom bar ([03d9c76](https://github.com/ardubev16/tmux-agents/commit/03d9c7621cc9083ad0885b5b5d24f81b51b0ca5e))
* **dashboard:** n highlights the next agent needing you; waiting agents show how long ([ec12c8f](https://github.com/ardubev16/tmux-agents/commit/ec12c8f581264150b14ebd8adfb30672a60e7fe6))
* **dashboard:** p lists the session's prompts and jumps the chat to one ([69e7284](https://github.com/ardubev16/tmux-agents/commit/69e7284934a1cb00eef303a4f93ad7be42e4d955))
* **dashboard:** show thoughts and tool calls as gray one-line steps that expand ([339e6c6](https://github.com/ardubev16/tmux-agents/commit/339e6c6bdba57fc4c745591e9c7cace19787e67a))
* **dashboard:** tab toggles the full context card, g opens lazygit; swap note/needs colors ([7359df2](https://github.com/ardubev16/tmux-agents/commit/7359df2471a4b6a7ea6b1cf8ae118cd764e88a9a))
* **integrations:** install only the harnesses named on the command line ([7c609c2](https://github.com/ardubev16/tmux-agents/commit/7c609c213b7185453dcb3df24ac16c0824a61f32))
* list agents as soon as their session opens, before the first prompt ([642def9](https://github.com/ardubev16/tmux-agents/commit/642def9e7d5f7a3e7e967dbdb8dabbba309e04a2))
* list the agent longest in its state first within each section ([7079d35](https://github.com/ardubev16/tmux-agents/commit/7079d35ba68ec164b0db226bea21a6a3fb742f1b))
* **status:** default background follows the status line ([0ace495](https://github.com/ardubev16/tmux-agents/commit/0ace4952448dd6ad95b9f6854055f0841cb1bb45))


### Bug Fixes

* dashboard text in the terminal's color, not popup-style's ([3ad7141](https://github.com/ardubev16/tmux-agents/commit/3ad714119bab72db7c5e7a2cb09ccbcfde9427a3))
* **dashboard:** give each project a distinct color, on its name only ([fdd3d6e](https://github.com/ardubev16/tmux-agents/commit/fdd3d6e88a71b0dfdb52f3159b899ceedd585f69))
* **dashboard:** hide the ? box's tooltip section for keys without one ([aff6f21](https://github.com/ardubev16/tmux-agents/commit/aff6f21fb4c19c52071b0964f4b2d0099fc565e5))
* **dashboard:** keep the ? box in place and show tooltips only where they add something ([e027d5c](https://github.com/ardubev16/tmux-agents/commit/e027d5ccd2430f6f461d3b4647a723c83574ce57))
* **dashboard:** keep the header's git summary clear of the scrollbar column ([46e10ff](https://github.com/ardubev16/tmux-agents/commit/46e10ff33c3fec1d12c410ca35c3b1292cf80729))
* **dashboard:** one column of padding right of the header's git summary ([2069ea9](https://github.com/ardubev16/tmux-agents/commit/2069ea94cbf619385298cf934173fa5722f3aede))
* **dashboard:** show an empty chat for a session not written yet, not the previous agent's ([f2dbc1a](https://github.com/ardubev16/tmux-agents/commit/f2dbc1a2445aaf1cf92a981f2b6b620333d5069d))
* **dashboard:** show the new conversation after omp /new and /clear ([62c80e3](https://github.com/ardubev16/tmux-agents/commit/62c80e301b91bf5ebd6da1b6e940dfa4c60e6536))
* **dashboard:** wrap the context card's fields; note in its own color ([13857ca](https://github.com/ardubev16/tmux-agents/commit/13857ca2fd8f4929059b4b198d8f99eec0cb27bb))
* **omp:** clear a queued prompt once the agent gets it mid-run ([6e0e739](https://github.com/ardubev16/tmux-agents/commit/6e0e739918da0df9f13aedd5033b393ef12d7836))
* **omp:** notice /move right away instead of on the next prompt ([8b18ca3](https://github.com/ardubev16/tmux-agents/commit/8b18ca33aa05d98202e38fc30b5333fced7a69c3))
* **omp:** report the session file again after /move ([dc15b68](https://github.com/ardubev16/tmux-agents/commit/dc15b689d8c029d9abd58c04942e62158e95b363))


### Reverts

* keep popup-style's text color in the dashboard ([c8765ea](https://github.com/ardubev16/tmux-agents/commit/c8765eaffcd9b2881e229d785723869e2bb0e14f))

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
