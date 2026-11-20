package main

import (
	"fmt"
	"net/http"
	"net/url"
)

// ---------------------------------------------------------------------------
// Languages: English is the default; Chinese (zh) is selectable. The choice
// is remembered in the syh_lang cookie. Pages are server-rendered, so each
// language gets its own parsed template set whose tr/trf/curlang functions
// close over that language (see main.go). Brand strings — SeeYouHere and the
// "I'm here" check-in button — are never translated.
// ---------------------------------------------------------------------------

const langCookieName = "syh_lang"
const langCookieMaxAge = 365 * 24 * 3600 // a year, well past the 30-day floor

func langOf(r *http.Request) string {
	if ck, err := r.Cookie(langCookieName); err == nil && ck.Value == "zh" {
		return "zh"
	}
	return "en"
}

// handleLang records the visitor's language choice, then sends them back
// to the page they were on.
func (a *app) handleLang(w http.ResponseWriter, r *http.Request) {
	set := r.URL.Query().Get("set")
	if set == "en" || set == "zh" {
		http.SetCookie(w, &http.Cookie{
			Name: langCookieName, Value: set, Path: "/", MaxAge: langCookieMaxAge,
			HttpOnly: true, Secure: a.cfg.cookieSecure(), SameSite: http.SameSiteLaxMode,
		})
	}
	target := "/"
	if ref := r.Referer(); ref != "" {
		if u, err := url.Parse(ref); err == nil && (u.Host == "" || u.Host == r.Host) && u.Path != "/lang" {
			target = u.RequestURI()
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

var dictEN = map[string]string{
	// shared
	"points":         "points",
	"privacy_link":   "Privacy",
	"footer_reset":   "One Space, one day — resets 08:00 UTC",
	"footer_gather":  "SeeYouHere · a tiny daily gathering",
	"sign_out":       "Sign out",
	"err_server":     "Server error.",
	"err_too_many":   "Too many attempts — please wait a bit.",
	"err_not_config": "Google sign-in is not configured yet.",

	// home — chrome
	"home_title": "SeeYouHere — a very boring social network where you can only tap names",

	// home — banned
	"banned_title": "This account is suspended.",
	"banned_body":  "You can look through the window, but you can’t step in or tap anyone.",

	// home — inside
	"in_prefix":      "You’re ",
	"in_word":        "in",
	"in_suffix":      ".",
	"here_pre":       "",
	"here_post":      " here today",
	"taps_left_pre":  "",
	"taps_left_mid":  " of ",
	"taps_left_post": " taps left today",
	"bar_left_pre":   "",
	"bar_left_post":  " taps left today",
	"ticket_one":     "ticket",
	"ticket_many":    "tickets",
	"tapped_you":     "Tapped you today:",
	"note_small_num": "The small number is how many times they tapped you today.",
	"todays_card":    "Today’s card",
	"copy_invite":    "Invite",
	"invite_copied":  "Invite link copied — a friend who steps in earns you a ticket",
	"admin_link":     "Admin",
	"something_wrong": "Something went wrong.",
	"network_hiccup":  "Network hiccup — try again.",
	"footer_day":      "day %d",

	// home — window / door
	"tagline":       "One Space. Everyone who’s here today.",
	"how_line":      "Tap 'I'm here', tap names, people tap you back. Your total never goes down.",
	"window_label":  "Through the window",
	"window_glimpse": "a glimpse, not the whole Space",
	"no_one_yet":    "no one here yet — be the first to arrive",
	"more_inside":   "…and %d more inside",
	"all_visible":   "everyone’s visible from here — for now",
	"outside_pre":   "You’re ",
	"outside_word":  "outside",
	"outside_post":  ", looking in.",
	"step_in_micro": "Step in to see everyone — and to be seen.",
	"continue_google": "Continue with Google",
	"signin_micro":  "Sign in, pick a name, then step in — see everyone, and be seen.",

	// setup
	"setup_title":   "Pick your name — SeeYouHere.today",
	"setup_tagline": "First, your name. It’s how everyone here will know you.",
	"setup_label":   "Your name in this Space",
	"setup_hint1":   "Letters, digits and Chinese characters only — no spaces, symbols or emoji.",
	"setup_hint2":   "Up to 16 units wide (a Chinese character counts as 2).",
	"width_prefix":  "Width so far: ",
	"width_suffix":  " / 16.",
	"setup_hint3":   "Names are unique: if someone has it, it’s theirs.",
	"continue_btn":  "Continue",
	"setup_footer":  "One name, for good.",
	"msg_choose":    "Please choose a name.",
	"msg_chars":     "Only letters, digits and Chinese characters — no spaces, symbols or emoji.",
	"msg_short":     "That name is too short.",
	"msg_long":      "That name is too long (max 16 units; a Chinese character counts as 2).",
	"msg_not_allowed": "That name isn’t allowed.",
	"msg_taken":     "That name is taken — names are unique, first come first served.",
	"msg_went_wrong": "Something went wrong — please choose your name again.",
	"msg_just_taken": "That name was just taken — please pick another.",

	// setup confirm
	"confirm_title":  "Confirm your name — SeeYouHere.today",
	"one_last_look":  "One last look.",
	"warn_pre":       "This will be your name here ",
	"warn_word":      "forever",
	"warn_post":      " — it can never be changed.",
	"warn2":          "If you ever want a different name, you’d need a different Google account, and nothing carries over.",
	"yes_me":         "Yes, that’s me",
	"go_back":        "Go back and pick another",
	"confirm_footer": "Choose once, choose well.",

	// share
	"share_title":      "Today’s card — SeeYouHere.today",
	"share_day":        "Day %d",
	"share_tapped_pre": "tapped ",
	"share_tapped_post": " times today",
	"share_points_pre": "",
	"share_points_post": " points, all time",
	"share_people_pre": "",
	"share_people_post": " people were here today",
	"share_copy_btn":   "Copy this card as text",
	"share_copied":     "Copied",
	"share_invite":     "Your invite link:",
	"share_invite_note": "When someone you invite steps in for the very first time, you earn a tap ticket. Tickets never expire.",
	"back_to_space":    "← Back to the Space",
	"share_text":       "Day %d · tapped %d times today · %d points · %d people were here — SeeYouHere.today",

	// tap API errors
	"tap_post_only":     "POST only",
	"err_signin_first":  "Sign in first.",
	"err_suspended":     "This account is suspended.",
	"err_bad_request":   "Bad request.",
	"err_self_tap":      "You can’t tap yourself.",
	"err_checkin_first": "Check in first.",
	"err_not_here":      "That person isn’t here.",
	"err_not_here_today": "That person isn’t here today.",
	"err_no_taps":       "No taps left today.",

	// auth flow errors
	"err_signin_expired":   "Sign-in session expired — please try again.",
	"err_signin_cancelled": "Sign-in was cancelled.",
	"err_reach_google":     "Could not reach Google — please try again.",
	"err_google_failed":    "Google sign-in failed — please try again.",

	// privacy
	"priv_title":   "Privacy — SeeYouHere.today",
	"priv_tagline": "Privacy, in plain words.",
	"priv_h_what":  "What SeeYouHere is",
	"priv_what":    "SeeYouHere is a small daily check-in experiment. Everyone who steps in on a given day shares one roster: you tap names, names tap you back, and the day resets at 08:00 UTC.",
	"priv_h_google": "Signing in with Google",
	"priv_google1_pre": "You sign in with Google so we never have to handle passwords. Of everything Google could tell us, we use and keep exactly one thing: a unique ID number for your account. We do ",
	"priv_google1_b":   "not",
	"priv_google1_post": " receive your email address, and we don’t use or store your Google name or photo.",
	"priv_google2": "The name other people see is the one you choose yourself at your first sign-in. It can never be changed afterwards — that’s a rule of the Space, not an oversight.",
	"priv_h_record": "What we record",
	"priv_rec1": "Your chosen name, and when your account was created.",
	"priv_rec2": "The days you check in (“I’m here”), and the time you checked in.",
	"priv_rec3": "Taps: who you tap and who taps you, counted per day.",
	"priv_rec4": "Your lifetime points (one for every tap you’ve ever received) and your referral ticket balance.",
	"priv_rec5": "How much of each day’s tap quota you’ve used.",
	"priv_rec6": "If you arrived through someone’s invite link, who invited you — they earn one ticket when you check in for the first time.",
	"priv_level": "Your level is worked out from your total number of check-in days, and it is shown only to you.",
	"priv_h_cookies": "Cookies",
	"priv_ck1": "A sign-in cookie (30 days), so you stay signed in.",
	"priv_ck2": "A short-lived cookie (10 minutes) that keeps the Google sign-in flow secure.",
	"priv_ck3": "If you arrive through an invite link, a referral cookie (30 days) so the invitation survives sign-in.",
	"priv_ck4": "If you browse while signed out, an anonymous cookie (up to a year) so your “through the window” glimpse stays the same between visits.",
	"priv_ck5": "A language cookie (up to a year) that remembers whether you read the site in English or Chinese.",
	"priv_ck_none": "No advertising cookies. No third-party tracking cookies.",
	"priv_h_dont": "What we don’t do",
	"priv_dont": "No ads. We never sell or rent your personal data, and we don’t share it with third parties — except the providers we need simply to run the site (our hosting and Cloudflare) and Google, which handles sign-in.",
	"priv_h_delete": "Keeping and deleting your data",
	"priv_delete_pre": "We keep your records while your account exists. If you’d like your account and its data deleted — or you have any privacy question at all — email ",
	"priv_delete_post": " and we’ll take care of it.",
	"priv_updated": "Last updated: November 18, 2026",
}

var dictZH = map[string]string{
	// shared
	"points":         "积分",
	"privacy_link":   "隐私",
	"footer_reset":   "同一个空间，同一天 — 每天 08:00 UTC 重置",
	"footer_gather":  "SeeYouHere · 一个小小的每日聚会",
	"sign_out":       "退出登录",
	"err_server":     "服务器出错了。",
	"err_too_many":   "尝试次数太多了 — 请稍等一会儿。",
	"err_not_config": "Google 登录还没配置好。",

	// home — chrome
	"home_title": "SeeYouHere.today — 同一个空间，今天在场的所有人。",

	// home — banned
	"banned_title": "这个账号已被停用。",
	"banned_body":  "你可以在窗外看看，但不能进来，也不能点任何人。",

	// home — inside
	"in_prefix":      "你已经",
	"in_word":        "进来了",
	"in_suffix":      "。",
	"here_pre":       "今天有 ",
	"here_post":      " 人在场",
	"taps_left_pre":  "今天还可点 ",
	"taps_left_mid":  " 次（共 ",
	"taps_left_post": " 次）",
	"bar_left_pre":   "今天还可点 ",
	"bar_left_post":  " 次",
	"ticket_one":     "张券",
	"ticket_many":    "张券",
	"tapped_you":     "今天点你的人次：",
	"note_small_num": "名字旁的小数字，是那个人今天点你的次数。",
	"todays_card":    "今日卡片",
	"copy_invite":    "邀请",
	"invite_copied":  "邀请链接已复制 — 朋友第一次进来，你得一张券",
	"admin_link":     "管理",
	"something_wrong": "出了点问题。",
	"network_hiccup":  "网络出了点小问题 — 再试一次。",
	"footer_day":      "第 %d 天",

	// home — window / door
	"tagline":       "同一个空间，今天在场的所有人。",
	"how_line":      "点“I'm here”，点别人的名字，别人也会点你。你的总分永不减少。",
	"window_label":  "隔着窗户看",
	"window_glimpse": "只是瞥一眼，不是整个空间",
	"no_one_yet":    "还没有人来 — 来做第一个到的",
	"more_inside":   "……里面还有 %d 人",
	"all_visible":   "暂时从这里能看到所有人",
	"outside_pre":   "你在",
	"outside_word":  "门外",
	"outside_post":  "，正往里看。",
	"step_in_micro": "进来看看所有人 — 也让大家看见你。",
	"continue_google": "用 Google 登录",
	"signin_micro":  "先登录，起个名字，再进来 — 看见所有人，也被看见。",

	// setup
	"setup_title":   "起个名字 — SeeYouHere.today",
	"setup_tagline": "先起个名字。这里的人都会用这个名字认识你。",
	"setup_label":   "你在这个空间的名字",
	"setup_hint1":   "只能用汉字、字母和数字 — 不能有空格、符号或表情。",
	"setup_hint2":   "宽度最多 16（一个汉字算 2）。",
	"width_prefix":  "当前宽度：",
	"width_suffix":  " / 16。",
	"setup_hint3":   "名字不重名：别人先占了，就是别人的。",
	"continue_btn":  "继续",
	"setup_footer":  "一个名字，定了就不改。",
	"msg_choose":    "请先起个名字。",
	"msg_chars":     "只能用汉字、字母和数字 — 不能有空格、符号或表情。",
	"msg_short":     "这个名字太短了。",
	"msg_long":      "这个名字太长了（最多 16 个单位；一个汉字算 2）。",
	"msg_not_allowed": "这个名字不能用。",
	"msg_taken":     "这个名字已经有人用了 — 名字不重名，先到先得。",
	"msg_went_wrong": "出了点问题 — 请重新起个名字。",
	"msg_just_taken": "这个名字刚刚被人占了 — 请换一个。",

	// setup confirm
	"confirm_title":  "确认你的名字 — SeeYouHere.today",
	"one_last_look":  "最后看一眼。",
	"warn_pre":       "这将是你在这里的名字，",
	"warn_word":      "永远",
	"warn_post":      " — 之后再也不能改。",
	"warn2":          "以后想换名字，只能换一个 Google 账号，而且一切都不会带过去。",
	"yes_me":         "对，这就是我",
	"go_back":        "回去换一个",
	"confirm_footer": "一次选择，认真选。",

	// share
	"share_title":      "今日卡片 — SeeYouHere.today",
	"share_day":        "第 %d 天",
	"share_tapped_pre": "今天被点了 ",
	"share_tapped_post": " 次",
	"share_points_pre": "",
	"share_points_post": " 积分，累计",
	"share_people_pre": "",
	"share_people_post": " 人今天来过",
	"share_copy_btn":   "以文字形式复制卡片",
	"share_copied":     "已复制",
	"share_invite":     "你的邀请链接：",
	"share_invite_note": "你邀请的人第一次进来时，你得一张点击券。券永不过期。",
	"back_to_space":    "← 回到空间",
	"share_text":       "第 %d 天 · 今天被点了 %d 次 · %d 积分 · 今天有 %d 人在场 — SeeYouHere.today",

	// tap API errors
	"tap_post_only":     "仅接受 POST",
	"err_signin_first":  "请先登录。",
	"err_suspended":     "这个账号已被停用。",
	"err_bad_request":   "请求有误。",
	"err_self_tap":      "不能点自己。",
	"err_checkin_first": "请先点 I'm here 进来。",
	"err_not_here":      "那个人不在这里。",
	"err_not_here_today": "那个人今天没来。",
	"err_no_taps":       "今天的点击次数用完了。",

	// auth flow errors
	"err_signin_expired":   "登录会话已过期 — 请重试。",
	"err_signin_cancelled": "登录已取消。",
	"err_reach_google":     "连不上 Google — 请重试。",
	"err_google_failed":    "Google 登录失败 — 请重试。",

	// privacy
	"priv_title":   "隐私 — SeeYouHere.today",
	"priv_tagline": "隐私，说人话版。",
	"priv_h_what":  "SeeYouHere 是什么",
	"priv_what":    "SeeYouHere 是一个小小的每日报到实验。同一天进来的人共用一份名单：你点别人的名字，别人也点你的，每天 08:00 UTC 重置。",
	"priv_h_google": "用 Google 登录",
	"priv_google1_pre": "你用 Google 登录，我们就不用经手密码。Google 能告诉我们的信息里，我们只取、也只留一样东西：你账号的一个唯一 ID 号。我们",
	"priv_google1_b":   "不会",
	"priv_google1_post": "收到你的邮箱地址，也不会使用或保存你的 Google 名字和头像。",
	"priv_google2": "别人看到的名字，是你第一次登录时自己起的。之后永远不能改 — 这是这个空间的规矩，不是疏忽。",
	"priv_h_record": "我们记录什么",
	"priv_rec1": "你起的名字，以及账号创建时间。",
	"priv_rec2": "你报到的日子（点“I'm here”），和报到的时间。",
	"priv_rec3": "点击：你点了谁、谁点了你，按天计数。",
	"priv_rec4": "你的终身积分（每收到一次点击得 1 分）和邀请券余额。",
	"priv_rec5": "你每天用掉的点击额度。",
	"priv_rec6": "如果你是通过别人的邀请链接来的，谁邀请了你 — 你第一次报到时，对方得一张券。",
	"priv_level": "你的等级按累计报到天数算出，且只显示给你自己。",
	"priv_h_cookies": "Cookie",
	"priv_ck1": "一个登录 cookie（30 天），让你保持登录状态。",
	"priv_ck2": "一个短期 cookie（10 分钟），保障 Google 登录流程的安全。",
	"priv_ck3": "如果你通过邀请链接来，一个邀请 cookie（30 天），让邀请在登录后依然算数。",
	"priv_ck4": "如果你在未登录时浏览，一个匿名 cookie（最长一年），让你每次“隔着窗户看”到的名单片段保持一致。",
	"priv_ck5": "一个语言 cookie（最长一年），记住你用英文还是中文看本站。",
	"priv_ck_none": "没有广告 cookie。没有第三方追踪 cookie。",
	"priv_h_dont": "我们不做什么",
	"priv_dont": "没有广告。我们从不出售或出租你的个人数据，也不会分享给第三方 — 除了运营本站必需的服务商（托管与 Cloudflare）和负责登录的 Google。",
	"priv_h_delete": "数据的保存与删除",
	"priv_delete_pre": "账号存在期间，我们保存你的记录。如果你想删除账号和数据 — 或有任何隐私问题 — 发邮件到 ",
	"priv_delete_post": "，我们会处理。",
	"priv_updated": "最后更新：2026 年 11 月 18 日",
}

var dicts = map[string]map[string]string{"en": dictEN, "zh": dictZH}

// tr looks up key in lang's dictionary, falling back to English, then the key.
func tr(lang, key string) string {
	if d, ok := dicts[lang]; ok {
		if s, ok := d[key]; ok {
			return s
		}
	}
	if s, ok := dictEN[key]; ok {
		return s
	}
	return key
}

// trf is tr with fmt.Sprintf applied.
func trf(lang, key string, args ...any) string {
	return fmt.Sprintf(tr(lang, key), args...)
}

// msgByEnglish translates a user-facing string that originates in Go code
// as an English literal (name-validation results) into the page language.
// English passes through untouched.
func msgByEnglish(lang, msg string) string {
	if lang != "zh" {
		return msg
	}
	switch msg {
	case "Please choose a name.":
		return tr(lang, "msg_choose")
	case "Only letters, digits and Chinese characters — no spaces, symbols or emoji.":
		return tr(lang, "msg_chars")
	case "That name is too short.":
		return tr(lang, "msg_short")
	case "That name is too long (max 16 units; a Chinese character counts as 2).":
		return tr(lang, "msg_long")
	case "That name isn't allowed.":
		return tr(lang, "msg_not_allowed")
	}
	return msg
}
