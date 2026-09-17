import { NextResponse } from "next/server";

// Identity spec §3.2/§4.2: Apple posts the id_token here (response_mode=form_post)
// from accounts.apple.com. Native mobile clients registered the chatapp:// deep
// link, so we bounce the token straight into the app; web users get a page that
// hands the token to POST /api/auth/apple client-side.
function forward(idToken: string, user?: string) {
  const target = new URL("chatapp://auth/apple");
  target.searchParams.set("id_token", idToken);
  if (user) target.searchParams.set("user", user);
  return target.toString();
}

export async function POST(req: Request) {
  const form = await req.formData();
  const idToken = String(form.get("id_token") ?? "");
  if (!idToken) {
    return NextResponse.redirect(
      new URL("/login?apple_error=missing_identity_token", req.url),
      303,
    );
  }
  // Apple sends the user's name/email only on the first authorization.
  let user: string | undefined;
  const rawUser = form.get("user");
  if (typeof rawUser === "string" && rawUser) user = rawUser;
  return NextResponse.redirect(forward(idToken, user), 303);
}
