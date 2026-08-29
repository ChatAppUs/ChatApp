export interface Media {
  kind: "image" | "video" | "audio";
  url: string;
  thumb_url?: string;
  width?: number;
  height?: number;
  duration_s?: number;
}

export interface Post {
  id: string;
  author_id: string;
  author_name: string;
  author_username: string;
  author_avatar: string;
  type: "post" | "reel" | "story";
  body: string;
  visibility: string;
  like_count: number;
  comment_count: number;
  share_count: number;
  view_count: number;
  liked_by_me: boolean;
  media: Media[];
  created_at: string;
}

export interface Comment {
  id: string;
  author_id: string;
  author_name: string;
  author_username: string;
  author_avatar: string;
  body: string;
  created_at: string;
}

export interface PublicUser {
  id: string;
  username: string;
  display_name: string;
  bio: string;
  avatar_url: string;
  locale: string;
  is_creator: boolean;
  is_verified: boolean;
  kyc_status?: string;
  created_at: string;
}

export interface Conversation {
  id: string;
  is_group: boolean;
  is_channel: boolean;
  title: string;
  created_at: string;
  last_message: string | null;
  unread: number;
}

export interface Message {
  id: string;
  sender_id: string;
  sender_name: string;
  body: string;
  media_url: string;
  is_encrypted?: boolean;
  reply_to?: string;
  created_at: string;
  edited_at?: string | null;
  reactions?: Record<string, number>;
}

export interface Channel {
  id: string;
  title: string;
  description: string;
  members: number;
  joined: boolean;
}

export interface TrendingTag {
  tag: string;
  count: number;
}

export interface PollOption {
  id: string;
  label: string;
  votes: number;
  voted_by_me: boolean;
}

export interface WalletAccount {
  id: string;
  asset: string;
  chain: string;
  address: string;
  balance: string;
}

export interface LedgerEntry {
  tx_id: string;
  asset: string;
  chain: string;
  amount: string;
  kind: string;
  memo: string;
  created_at: string;
}

export interface Country {
  name: string;
  iso: string;
  dial: string;
  flag: string;
}

export interface Campaign {
  id: string;
  name: string;
  objective: string;
  status: string;
  daily_budget: string;
  total_budget: string;
  spent: string;
  currency: string;
  target_countries: string[];
  target_locales: string[];
  created_at: string;
}
