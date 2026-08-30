// Typed client for the feature surface added on top of the core API:
// push, watch signals, groups/pages/events, monetization, bots, privacy,
// highlights. Each maps 1:1 to the Go API routes; Android, iOS and desktop
// implement the same set.

import { api } from "./api";

// ---- push notifications ----
export interface PushSubscriptionJSON {
  endpoint: string;
  keys?: { p256dh?: string; auth?: string };
}
export const registerWebPush = (sub: PushSubscriptionJSON, ua: string) =>
  api("/api/push/subscribe", { method: "POST", body: JSON.stringify({ subscription: sub, user_agent: ua }) });
export const unregisterWebPush = (endpoint: string) =>
  api("/api/push/unsubscribe", { method: "POST", body: JSON.stringify({ endpoint }) });
export const vapidPublicKey = () =>
  api<{ key: string }>("/api/push/public-key", {}, true);

// ---- watch signals / For You page ----
export interface WatchSignal {
  watched_ms: number;
  duration_ms: number;
  completed: boolean;
  rewatched: boolean;
  not_interested?: boolean;
}
export const sendWatchSignal = (postId: string, s: WatchSignal) =>
  api(`/api/reels/${postId}/watch`, { method: "POST", body: JSON.stringify(s) });
export const fypFeed = (limit = 20, offset = 0) =>
  api<{ posts: FypPost[] }>(`/api/fyp?limit=${limit}&offset=${offset}`);
export interface FypPost {
  id: string;
  author_id: string;
  display_name: string;
  username: string;
  avatar_url: string;
  body: string;
  like_count: number;
  comment_count: number;
  view_count: number;
  created_at: string;
  media_url?: string;
  watch_pct?: number;
  completion_rate?: number;
}

// ---- groups ----
export interface Group {
  id: string;
  name: string;
  slug: string;
  description: string;
  cover_url: string;
  privacy: string;
  member_count: number;
  created_at: string;
}
export interface GroupDetail extends Group {
  created_by: string;
  my_role: string;
  members: GroupMember[];
}
export interface GroupMember {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string;
  role: string;
}
export const createGroup = (name: string, description: string, isPrivate: boolean) =>
  api<{ id: string; slug: string }>("/api/groups", {
    method: "POST",
    body: JSON.stringify({ name, description, privacy: isPrivate ? "private" : "public" }),
  });
export const listGroups = (q = "", limit = 20, offset = 0) =>
  api<{ groups: Group[] }>(`/api/groups?q=${encodeURIComponent(q)}&limit=${limit}&offset=${offset}`);
export const getGroup = (id: string) => api<GroupDetail>(`/api/groups/${id}`);
export const joinGroup = (id: string) =>
  api<{ status: string }>(`/api/groups/${id}/join`, { method: "POST" });
export const leaveGroup = (id: string) =>
  api(`/api/groups/${id}/join`, { method: "DELETE" });
export const groupFeed = (id: string, limit = 20, offset = 0) =>
  api<{ posts: { id: string; body: string; like_count: number; comment_count: number; created_at: string }[] }>(
    `/api/groups/${id}/feed?limit=${limit}&offset=${offset}`);
export const postToGroup = (id: string, body: string) =>
  api(`/api/groups/${id}/posts`, { method: "POST", body: JSON.stringify({ body }) });

// ---- events ----
export interface EventItem {
  id: string;
  title: string;
  description: string;
  location: string;
  starts_at: string;
  ends_at?: string | null;
  group_id: string;
  page_id: string;
  going_count: number;
}
export const listEvents = (limit = 20, offset = 0) =>
  api<{ events: EventItem[] }>(`/api/events?limit=${limit}&offset=${offset}`);
export const getEvent = (id: string) =>
  api<EventItem & { rsvp_counts: Record<string, number>; my_response: string }>(`/api/events/${id}`);
export const createEvent = (opts: { title: string; description: string; location: string; starts_at: string; group_id?: string; page_id?: string }) =>
  api<{ id: string }>("/api/events", { method: "POST", body: JSON.stringify(opts) });
export const rsvpEvent = (id: string, response: "going" | "interested" | "declined") =>
  api<{ status: string }>(`/api/events/${id}/rsvp`, { method: "POST", body: JSON.stringify({ response }) });

// ---- pages ----
export interface Page {
  id: string;
  name: string;
  slug: string;
  category: string;
  description: string;
  avatar_url: string;
  cover_url: string;
  follower_count: number;
  created_at: string;
}
export interface PageDetail extends Page {
  owner_id: string;
  following: boolean;
}
export const createPage = (name: string, category: string, description: string) =>
  api<{ id: string; slug: string }>("/api/pages", {
    method: "POST",
    body: JSON.stringify({ name, category, description }),
  });
export const listPages = (q = "", limit = 20, offset = 0) =>
  api<{ pages: Page[] }>(`/api/pages?q=${encodeURIComponent(q)}&limit=${limit}&offset=${offset}`);
export const getPage = (id: string) => api<PageDetail>(`/api/pages/${id}`);
export const followPage = (id: string) =>
  api<{ status: string }>(`/api/pages/${id}/follow`, { method: "POST" });
export const unfollowPage = (id: string) =>
  api(`/api/pages/${id}/follow`, { method: "DELETE" });
export const pageFeed = (id: string, limit = 20, offset = 0) =>
  api<{ posts: { id: string; body: string; like_count: number; comment_count: number; created_at: string }[] }>(
    `/api/pages/${id}/feed?limit=${limit}&offset=${offset}`);
export const postToPage = (id: string, body: string) =>
  api(`/api/pages/${id}/posts`, { method: "POST", body: JSON.stringify({ body }) });

// ---- monetization ----
export interface Tier {
  id: string;
  name: string;
  perks: string;
  price_usd: number;
  subscriber_count: number;
  created_at: string;
}
export const createTier = (name: string, perks: string, priceUsd: number) =>
  api<{ id: string }>("/api/creator/tiers", {
    method: "POST",
    body: JSON.stringify({ name, perks, price_usd: priceUsd }),
  });
export const myTiers = () => api<{ tiers: Tier[] }>("/api/creator/tiers");
export const creatorTiers = (userId: string) =>
  api<{ tiers: Tier[] }>(`/api/users/${userId}/tiers`);
export const deleteTier = (id: string) => api(`/api/creator/tiers/${id}`, { method: "DELETE" });
export const subscribe = (tierId: string) =>
  api<{ status: string; tx_id: string }>(`/api/tiers/${tierId}/subscribe`, { method: "POST" });
export const cancelSubscription = (subscriptionId: string) =>
  api(`/api/subscriptions/${subscriptionId}`, { method: "DELETE" });
export interface Subscription {
  id: string;
  tier_name: string;
  price_usd: number;
  creator_username: string;
  creator_display_name: string;
  status: string;
  current_period_end: string;
  created_at: string;
}
export const mySubscriptions = () =>
  api<{ subscriptions: Subscription[] }>("/api/subscriptions");
export interface Earnings {
  earned: number;
  paid_out: number;
  available: number;
  currency: string;
}
export const earnings = () => api<Earnings>("/api/creator/earnings");
export const sendTip = (userId: string, amountUsd: number, message: string) =>
  api<{ status: string; tx_id: string }>(`/api/users/${userId}/tip`, {
    method: "POST",
    body: JSON.stringify({ amount_usd: amountUsd, message }),
  });
export interface Gift {
  id: string;
  name: string;
  price_usd: number;
  asset: string;
}
export const giftCatalog = () => api<{ gifts: Gift[] }>("/api/gifts");
export const sendGift = (userId: string, giftId: string) =>
  api<{ status: string; tx_id: string }>(`/api/users/${userId}/gift`, {
    method: "POST",
    body: JSON.stringify({ gift_id: giftId }),
  });

// ---- bots ----
export interface Bot {
  id: string;
  username: string;
  display_name: string;
  description: string;
  active: boolean;
  has_webhook: boolean;
  mini_app_url: string;
  created_at: string;
}
export const createBot = (username: string, displayName: string, description: string) =>
  api<{ id: string; user_id: string; username: string; token: string }>("/api/bots", {
    method: "POST",
    body: JSON.stringify({ username, display_name: displayName, description }),
  });
export const myBots = () => api<{ bots: Bot[] }>("/api/bots");
export const deleteBot = (id: string) => api(`/api/bots/${id}`, { method: "DELETE" });
export const setWebhook = (id: string, url: string) =>
  api(`/api/bots/${id}/webhook`, { method: "POST", body: JSON.stringify({ url }) });
export const setMiniApp = (id: string, title: string, url: string) =>
  api(`/api/bots/${id}/mini-app`, { method: "POST", body: JSON.stringify({ title, url }) });

// ---- privacy ----
export const mute = (userId: string) => api(`/api/users/${userId}/mute`, { method: "POST" });
export const unmute = (userId: string) => api(`/api/users/${userId}/mute`, { method: "DELETE" });
export const listMutes = () => api<{ mutes: MutedUser[] }>("/api/me/mutes");
export interface MutedUser {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string;
}
export const restrict = (userId: string) =>
  api(`/api/users/${userId}/restrict`, { method: "POST" });
export const unrestrict = (userId: string) =>
  api(`/api/users/${userId}/restrict`, { method: "DELETE" });
export const listRestricted = () =>
  api<{ restricted: MutedUser[] }>("/api/me/restricted");
export const addWordFilter = (phrase: string) =>
  api(`/api/me/word-filters`, { method: "POST", body: JSON.stringify({ phrase }) });
export const removeWordFilter = (phrase: string) =>
  api(`/api/me/word-filters`, { method: "DELETE", body: JSON.stringify({ phrase }) });
export const listWordFilters = () =>
  api<{ filters: { phrase: string; created_at: string }[] }>("/api/me/word-filters");
export const setProfileLock = (locked: boolean) =>
  api<{ profile_locked: boolean }>(`/api/me/profile-lock`, { method: "PUT", body: JSON.stringify({ locked }) });
export const setActiveStatus = (show: boolean) =>
  api<{ show_active_status: boolean }>(`/api/me/active-status`, { method: "PUT", body: JSON.stringify({ show }) });
export interface FollowRequest {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string;
  requested_at: string;
}
export const followRequests = () =>
  api<{ requests: FollowRequest[] }>("/api/me/follow-requests");
export const acceptFollowRequest = (uid: string) =>
  api(`/api/me/follow-requests/${uid}/accept`, { method: "POST" });
export const declineFollowRequest = (uid: string) =>
  api(`/api/me/follow-requests/${uid}/decline`, { method: "POST" });
export interface MessageRequest {
  conversation_id: string;
  username: string;
  display_name: string;
  avatar_url: string;
  preview: string | null;
  requested_at: string;
}
export const messageRequests = () =>
  api<{ requests: MessageRequest[] }>("/api/me/message-requests");
export const acceptMessageRequest = (convId: string) =>
  api(`/api/me/message-requests/${convId}/accept`, { method: "POST" });
export const declineMessageRequest = (convId: string) =>
  api(`/api/me/message-requests/${convId}/decline`, { method: "POST" });

// ---- stories extras ----
export const closeFriends = () => api<{ close_friends: MutedUser[] }>("/api/me/close-friends");
export const addCloseFriend = (uid: string) =>
  api(`/api/users/${uid}/close-friend`, { method: "POST" });
export const removeCloseFriend = (uid: string) =>
  api(`/api/users/${uid}/close-friend`, { method: "DELETE" });
export interface Highlight {
  id: string;
  title: string;
  cover_url: string;
  story_count: number;
  created_at: string;
}
export const createHighlight = (title: string, coverUrl: string) =>
  api<{ id: string }>("/api/highlights", { method: "POST", body: JSON.stringify({ title, cover_url: coverUrl }) });
export const myHighlights = () => api<{ highlights: Highlight[] }>("/api/highlights");
export const userHighlights = (uid: string) =>
  api<{ highlights: Highlight[] }>(`/api/users/${uid}/highlights`);
export const deleteHighlight = (id: string) => api(`/api/highlights/${id}`, { method: "DELETE" });
export const addHighlightStory = (highlightId: string, storyId: string) =>
  api(`/api/highlights/${highlightId}/items/${storyId}`, { method: "POST" });
export const removeHighlightStory = (highlightId: string, storyId: string) =>
  api(`/api/highlights/${highlightId}/items/${storyId}`, { method: "DELETE" });
