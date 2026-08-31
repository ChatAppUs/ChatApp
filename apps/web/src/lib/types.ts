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
  my_reaction?: string;
  feeling?: string;
  location?: string;
  tagged_usernames?: string[];
  media: Media[];
  created_at: string;
  publish_at?: string | null;
  repost_of?: string;
  thread_parent_id?: string;
  edited_at?: string | null;
  story_background?: string;
  story_stickers?: string;
  story_music?: string;
  quoted?: { id: string; author_name: string; author_username: string; body: string } | null;
  remix_of?: string;
  remix_mode?: "duet" | "stitch";
}

export const REACTIONS: Record<string, string> = {
  like: "👍",
  love: "❤️",
  haha: "😂",
  wow: "😮",
  sad: "😢",
  angry: "😡",
};

export interface Card {
  id: string;
  label: string;
  last4: string;
  expiry_month: number;
  expiry_year: number;
  status: string;
  daily_limit_usd: string;
  monthly_limit_usd: string;
  balance_usd: string;
  created_at: string;
}

export interface CardTransaction {
  id: string;
  merchant: string;
  amount_usd: string;
  kind: string;
  status: string;
  created_at: string;
}

export interface Merchant {
  user_id: string;
  username: string;
  business_name: string;
  status: string;
  tier: number;
  tier_name: string;
  note: string;
  applied_at: string;
  decided_at?: string | null;
}

export interface MerchantTier {
  level: number;
  name: string;
  max_trade_usd: string;
  daily_volume_usd: string;
  min_completed_trades: number;
  min_completion_rate: string;
}

export interface Album {
  id: string;
  title: string;
  description: string;
  cover_url: string;
  item_count: number;
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
  is_merchant?: boolean;
  merchant_tier?: number;
  pinned_post_id?: string;
  kyc_status?: string;
  created_at: string;
}

export interface Conversation {
  id: string;
  is_group: boolean;
  is_channel: boolean;
  title: string;
  created_at: string;
  theme?: string;
  last_message: string | null;
  unread: number;
}

export interface ConvMember {
  id: string;
  username: string;
  display_name: string;
  role: string;
  joined_at: string;
  nickname?: string;
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
  kind?: string;
  poll_id?: string;
  payment_id?: string;
}

export interface PollOption {
  id: string;
  label: string;
  votes: number;
  my_vote: boolean;
}

export interface ChatPollState {
  id: string;
  question: string;
  multi: boolean;
  closes_at: string | null;
  options: PollOption[];
  total_votes: number;
  is_quiz?: boolean;
  anonymous?: boolean;
  correct_option_id?: string;
  explanation?: string;
}

export interface LiveLocation {
  user_id: string;
  username: string;
  lat: number;
  lng: number;
  expires_at: string;
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
  owner_is_merchant?: boolean;
  owner_merchant_tier?: number;
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


export interface StakingAsset {
  asset: string;
  chain: string;
  apy: string;
  durations: number[];
  min_amount: string;
  max_amount: string;
  price_usd: string | null;
}

export interface StakingPosition {
  id: string;
  asset: string;
  chain: string;
  amount: string;
  apy: string;
  duration_days: number;
  started_at: string;
  ends_at: string;
  status: string;
  reward?: string | null;
  accrued_estimate?: string | null;
  closed_at?: string | null;
}

export interface TokenPrice {
  asset: string;
  chain: string;
  usd: string | null;
  source: string | null;
  fetched_at: string | null;
}
