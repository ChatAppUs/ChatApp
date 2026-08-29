"use client";

import React, { createContext, useContext, useEffect, useState } from "react";

export const LOCALES = [
  { code: "en", name: "English", dir: "ltr" },
  { code: "es", name: "Español", dir: "ltr" },
  { code: "fr", name: "Français", dir: "ltr" },
  { code: "de", name: "Deutsch", dir: "ltr" },
  { code: "pt", name: "Português", dir: "ltr" },
  { code: "ar", name: "العربية", dir: "rtl" },
  { code: "hi", name: "हिन्दी", dir: "ltr" },
  { code: "zh", name: "中文", dir: "ltr" },
] as const;

export type LocaleCode = (typeof LOCALES)[number]["code"];

type Dict = Record<string, string>;

const en: Dict = {
  appName: "ChatApp",
  feed: "Feed",
  reels: "Reels",
  chat: "Chat",
  wallet: "Wallet",
  creator: "Creator",
  ads: "Ads",
  login: "Log in",
  logout: "Log out",
  register: "Create account",
  username: "Username",
  email: "Email",
  phone: "Phone",
  password: "Password",
  displayName: "Display name",
  forgotPassword: "Forgot password?",
  resetPassword: "Reset password",
  sendResetLink: "Send reset link",
  newPassword: "New password",
  whatsOnYourMind: "What's on your mind?",
  save: "Save",
  cancel: "Cancel",
  post: "Post",
  story: "Story",
  reel: "Reel",
  like: "Like",
  comment: "Comment",
  share: "Share",
  comments: "Comments",
  writeComment: "Write a comment…",
  send: "Send",
  typeMessage: "Type a message…",
  newChat: "New chat",
  balance: "Balance",
  transfer: "Transfer",
  toUsername: "To (username)",
  amount: "Amount",
  asset: "Asset",
  chain: "Chain",
  memo: "Memo",
  history: "History",
  createAccount: "Add asset account",
  kyc: "Identity verification",
  kycSubmit: "Submit KYC",
  kycStatus: "KYC status",
  fullName: "Full name",
  country: "Country",
  docNumber: "Document number",
  campaigns: "Campaigns",
  newCampaign: "New campaign",
  campaignName: "Campaign name",
  budget: "Budget",
  targetCountries: "Target countries",
  submitForReview: "Submit for review",
  addCreative: "Add creative",
  fund: "Fund",
  searchUsers: "Search users…",
  follow: "Follow",
  unfollow: "Unfollow",
  followers: "Followers",
  following: "Following",
  notifications: "Notifications",
  startCall: "Start call",
  videoCall: "Video call",
  audioCall: "Audio call",
  endCall: "End call",
  stats: "Stats",
  users: "Users",
  reports: "Reports",
  kycQueue: "KYC queue",
  adReview: "Ad review",
  approve: "Approve",
  reject: "Reject",
  suspend: "Suspend",
  activate: "Activate",
  resolve: "Resolve",
  dismiss: "Dismiss",
  loading: "Loading…",
  noResults: "Nothing here yet",
  error: "Something went wrong",
  phoneVerify: "Phone verification",
  sendCode: "Send code",
  verifyCode: "Verify code",
  code: "Code",
  selectCountry: "Select country",
  uploadMedia: "Attach photo/video",
  resetSent: "If the email exists, a reset link was sent.",
  passwordUpdated: "Password updated. You can log in now.",
  creatorStudio: "Creator studio",
  earnings: "Earnings",
};

const es: Dict = {
  ...en,
  feed: "Inicio", reels: "Reels", chat: "Chat", wallet: "Billetera", creator: "Creador", ads: "Anuncios",
  login: "Iniciar sesión", logout: "Cerrar sesión", register: "Crear cuenta",
  username: "Usuario", email: "Correo", phone: "Teléfono", password: "Contraseña",
  displayName: "Nombre visible", forgotPassword: "¿Olvidaste tu contraseña?",
  resetPassword: "Restablecer contraseña", sendResetLink: "Enviar enlace",
  newPassword: "Nueva contraseña", whatsOnYourMind: "¿Qué estás pensando?",
  post: "Publicar", story: "Historia", reel: "Reel", like: "Me gusta", comment: "Comentar",
  share: "Compartir", comments: "Comentarios", writeComment: "Escribe un comentario…",
  send: "Enviar", typeMessage: "Escribe un mensaje…", newChat: "Nuevo chat",
  balance: "Saldo", transfer: "Transferir", amount: "Cantidad", history: "Historial",
  follow: "Seguir", unfollow: "Dejar de seguir", followers: "Seguidores", following: "Siguiendo",
  startCall: "Llamar", videoCall: "Videollamada", audioCall: "Llamada de voz", endCall: "Colgar",
  loading: "Cargando…", searchUsers: "Buscar usuarios…",
};

const fr: Dict = {
  ...en,
  feed: "Fil", chat: "Discussion", wallet: "Portefeuille", creator: "Créateur", ads: "Publicités",
  login: "Connexion", logout: "Déconnexion", register: "Créer un compte",
  username: "Nom d'utilisateur", email: "E-mail", phone: "Téléphone", password: "Mot de passe",
  forgotPassword: "Mot de passe oublié ?", whatsOnYourMind: "Quoi de neuf ?",
  post: "Publier", like: "J'aime", comment: "Commenter", share: "Partager",
  send: "Envoyer", typeMessage: "Écrivez un message…", follow: "Suivre",
  unfollow: "Ne plus suivre", loading: "Chargement…",
};

const de: Dict = {
  ...en,
  feed: "Startseite", chat: "Chat", wallet: "Wallet", creator: "Creator", ads: "Werbung",
  login: "Anmelden", logout: "Abmelden", register: "Konto erstellen",
  password: "Passwort", post: "Posten", like: "Gefällt mir", comment: "Kommentieren",
  send: "Senden", follow: "Folgen", unfollow: "Entfolgen", loading: "Laden…",
};

const pt: Dict = {
  ...en,
  feed: "Início", chat: "Conversas", wallet: "Carteira", creator: "Criador", ads: "Anúncios",
  login: "Entrar", logout: "Sair", register: "Criar conta", password: "Senha",
  post: "Publicar", like: "Curtir", comment: "Comentar", send: "Enviar",
  follow: "Seguir", unfollow: "Deixar de seguir", loading: "Carregando…",
};

const ar: Dict = {
  ...en,
  feed: "الرئيسية", reels: "ريلز", chat: "الدردشة", wallet: "المحفظة", creator: "منشئ المحتوى", ads: "الإعلانات",
  login: "تسجيل الدخول", logout: "تسجيل الخروج", register: "إنشاء حساب",
  username: "اسم المستخدم", email: "البريد الإلكتروني", phone: "الهاتف", password: "كلمة المرور",
  whatsOnYourMind: "بماذا تفكر؟", post: "نشر", like: "إعجاب", comment: "تعليق",
  share: "مشاركة", send: "إرسال", follow: "متابعة", unfollow: "إلغاء المتابعة",
  loading: "جارٍ التحميل…", startCall: "بدء مكالمة", endCall: "إنهاء المكالمة",
};

const hi: Dict = {
  ...en,
  feed: "फ़ीड", chat: "चैट", wallet: "वॉलेट", creator: "क्रिएटर", ads: "विज्ञापन",
  login: "लॉग इन", logout: "लॉग आउट", register: "खाता बनाएं", password: "पासवर्ड",
  post: "पोस्ट", like: "पसंद", comment: "टिप्पणी", send: "भेजें",
  follow: "फ़ॉलो", unfollow: "अनफ़ॉलो", loading: "लोड हो रहा है…",
};

const zh: Dict = {
  ...en,
  feed: "动态", reels: "短视频", chat: "聊天", wallet: "钱包", creator: "创作者", ads: "广告",
  login: "登录", logout: "退出", register: "注册",
  username: "用户名", email: "邮箱", phone: "手机号", password: "密码",
  post: "发布", like: "赞", comment: "评论", share: "分享", send: "发送",
  follow: "关注", unfollow: "取消关注", loading: "加载中…",
};

const DICTS: Record<LocaleCode, Dict> = { en, es, fr, de, pt, ar, hi, zh };

interface I18nCtx {
  locale: LocaleCode;
  setLocale: (l: LocaleCode) => void;
  t: (key: string) => string;
  dir: "ltr" | "rtl";
}

const Ctx = createContext<I18nCtx>({
  locale: "en",
  setLocale: () => {},
  t: (k) => k,
  dir: "ltr",
});

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<LocaleCode>("en");

  useEffect(() => {
    const saved = localStorage.getItem("chatapp.locale") as LocaleCode | null;
    if (saved && saved in DICTS) setLocaleState(saved);
  }, []);

  useEffect(() => {
    const dir = LOCALES.find((l) => l.code === locale)?.dir ?? "ltr";
    document.documentElement.lang = locale;
    document.documentElement.dir = dir;
  }, [locale]);

  const setLocale = (l: LocaleCode) => {
    localStorage.setItem("chatapp.locale", l);
    setLocaleState(l);
  };

  const t = (key: string) => DICTS[locale][key] ?? en[key] ?? key;
  const dir = (LOCALES.find((l) => l.code === locale)?.dir ?? "ltr") as "ltr" | "rtl";

  return <Ctx.Provider value={{ locale, setLocale, t, dir }}>{children}</Ctx.Provider>;
}

export function useI18n() {
  return useContext(Ctx);
}
