"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { useI18n } from "@/lib/i18n";
import {
  createEvent, getGroup, groupFeed, joinGroup, leaveGroup, listEvents,
  postToGroup, rsvpEvent, type EventItem, type GroupDetail,
} from "@/lib/features";

interface GroupPost { id: string; body: string; like_count: number; comment_count: number; created_at: string }

export default function GroupDetailPage() {
  const { t } = useI18n();
  const params = useParams<{ id: string }>();
  const groupId = params.id;
  const [group, setGroup] = useState<GroupDetail | null>(null);
  const [posts, setPosts] = useState<GroupPost[]>([]);
  const [events, setEvents] = useState<EventItem[]>([]);
  const [error, setError] = useState("");
  const [postBody, setPostBody] = useState("");
  const [title, setTitle] = useState("");
  const [desc, setDesc] = useState("");
  const [startsAt, setStartsAt] = useState("");
  const [location, setLocation] = useState("");

  const load = useCallback(async () => {
    try {
      const [g, f, e] = await Promise.all([
        getGroup(groupId), groupFeed(groupId, 20, 0), listEvents(50, 0),
      ]);
      setGroup(g);
      setPosts(f.posts);
      setEvents(e.events.filter((ev) => ev.group_id === groupId));
    } catch (err) {
      setError(err instanceof Error ? err.message : "load failed");
    }
  }, [groupId]);

  useEffect(() => { load(); }, [load]);

  const join = async () => { await joinGroup(groupId).catch((e) => setError(e.message)); load(); };
  const leave = async () => { await leaveGroup(groupId).catch((e) => setError(e.message)); load(); };

  const post = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await postToGroup(groupId, postBody);
      setPostBody("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "post failed");
    }
  };

  const addEvent = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await createEvent({
        title, description: desc, location,
        starts_at: new Date(startsAt).toISOString(), group_id: groupId,
      });
      setTitle(""); setDesc(""); setStartsAt(""); setLocation("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "event failed");
    }
  };

  const rsvp = async (id: string, response: "going" | "interested" | "declined") => {
    await rsvpEvent(id, response).catch((e) => setError(e.message));
    load();
  };

  const member = group && group.my_role !== "";

  return (
    <div className="col">
      <h1>{group ? group.name : t("groups")}</h1>
      {error && <div className="error">{error}</div>}
      {group && (
        <div className="card col">
          <div className="row">
            <span className="badge">{group.member_count} · {group.privacy}</span>
            {group.my_role && <span className="badge green">{group.my_role}</span>}
          </div>
          {group.description && <p className="muted">{group.description}</p>}
          <div className="row">
            {!member && <button onClick={join}>{t("joinGroup")}</button>}
            {member && group.my_role !== "owner" && (
              <button className="secondary" onClick={leave}>{t("leaveGroup")}</button>
            )}
          </div>
        </div>
      )}

      {member && (
        <>
          <h2>{t("events")}</h2>
          <form className="card col" onSubmit={addEvent}>
            <h3>{t("createEvent")}</h3>
            <label>{t("eventTitle")}</label>
            <input value={title} onChange={(e) => setTitle(e.target.value)} required maxLength={140} />
            <label>{t("description") ?? t("groupTopic")}</label>
            <input value={desc} onChange={(e) => setDesc(e.target.value)} maxLength={2048} />
            <label>{t("startTime")}</label>
            <input type="datetime-local" value={startsAt} onChange={(e) => setStartsAt(e.target.value)} required />
            <label>{t("location")}</label>
            <input value={location} onChange={(e) => setLocation(e.target.value)} maxLength={256} />
            <button type="submit">{t("createEvent")}</button>
          </form>
          {events.map((ev) => (
            <div className="card col" key={ev.id}>
              <div className="row">
                <strong>{ev.title}</strong>
                <span className="badge">{new Date(ev.starts_at).toLocaleString()}</span>
              </div>
              {ev.location && <p className="muted">{ev.location}</p>}
              <div className="row">
                <span className="badge green">{ev.going_count} {t("rsvpGoing").toLowerCase()}</span>
                <button className="secondary" onClick={() => rsvp(ev.id, "going")}>{t("rsvpGoing")}</button>
                <button className="secondary" onClick={() => rsvp(ev.id, "interested")}>{t("rsvpInterested")}</button>
                <button className="secondary" onClick={() => rsvp(ev.id, "declined")}>{t("rsvpNo")}</button>
              </div>
            </div>
          ))}

          <h2>{t("feed")}</h2>
          <form className="card row" onSubmit={post}>
            <input placeholder={t("postPlaceholder") ?? "…"} value={postBody}
              onChange={(e) => setPostBody(e.target.value)} required maxLength={5000} />
            <button type="submit">{t("post") ?? "Post"}</button>
          </form>
        </>
      )}
      {posts.map((p) => (
        <div className="card col" key={p.id}>
          <p>{p.body}</p>
          <span className="muted">{p.like_count} · {p.comment_count} · {new Date(p.created_at).toLocaleString()}</span>
        </div>
      ))}
      {posts.length === 0 && <p className="muted">—</p>}
    </div>
  );
}
