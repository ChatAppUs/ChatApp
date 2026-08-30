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
  repost_of?: string;
  thread_parent_id?: string;
  edited_at?: string | null;
  quoted?: { id: string; author_name: string; author_username: string; body: string } | null;
}

export interface Comment {
  id: string;
  author_id: string;
  author_name: string;
  author_username: string;
  author_avatar: string;
  body: string;
  created_at: string;
  parent_id?: string;
  like_count: number;
  liked_by_me: boolean;
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
  forwarded_from?: string;
  story_id?: string;
  pinned?: boolean;
  expires_at?: string | null;
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

export interface DepositAddress {
  asset: string;
  chain: string;
  address: string;
  uri: string;
}

export interface Withdrawal {
  id: string;
  asset: string;
  chain: string;
  to_address: string;
  amount: string;
  fee: string;
  status: string;
  auto_approved: boolean;
  tx_hash: string;
  created_at: string;
  updated_at: string;
}

export interface ConvertRate {
  asset: string;
  chain: string;
  usd_rate: string;
  updated_at: string;
}

export interface Conversion {
  id: string;
  from_asset: string;
  from_chain: string;
  to_asset: string;
  to_chain: string;
  from_amount: string;
  to_amount: string;
  rate: string;
  created_at: string;
}

export interface P2PPaymentMethod {
  country_iso: string;
  name: string;
  kind: string;
}

export interface P2POffer {
  id: string;
  owner_username: string;
  side: string;
  asset: string;
  chain: string;
  fiat_currency: string;
  country_iso: string;
  price: string;
  min_amount: string;
  max_amount: string;
  payment_methods: string[];
  terms: string;
  active: boolean;
  created_at: string;
}

export interface P2PTrade {
  id: string;
  offer_id: string;
  buyer_username: string;
  seller_username: string;
  asset: string;
  chain: string;
  crypto_amount: string;
  fiat_amount: string;
  fiat_currency: string;
  payment_method: string;
  status: string;
  created_at: string;
  paid_at: string | null;
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
