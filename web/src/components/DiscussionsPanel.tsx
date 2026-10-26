// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { listDiscussions, newClientMessageID, postDiscussion, type Discussion } from "../api";
import { ErrorNotice, StatusNotice } from "../errors";
import { useI18n } from "../i18n";

export default function DiscussionsPanel({ objectID }: { objectID: string }) {
  const { t, lang } = useI18n();
  const [items, setItems] = useState<Discussion[]>([]);
  const [draft, setDraft] = useState("");
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);

  const timeFormatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });

  const load = useCallback(
    async (cursor?: string, signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const page = await listDiscussions(objectID, cursor, signal);
        if (signal?.aborted) {
          return;
        }
        setItems((previous) => (cursor ? [...previous, ...page.items] : page.items));
        setNextCursor(page.next_cursor);
      } catch (failure) {
        if (!signal?.aborted) {
          setError(failure);
        }
      } finally {
        if (!signal?.aborted) {
          setLoading(false);
        }
      }
    },
    [objectID]
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(undefined, controller.signal);
    return () => controller.abort();
  }, [load]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!draft.trim()) {
      return;
    }
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      await postDiscussion(objectID, draft, newClientMessageID());
      setDraft("");
      setNotice(t("discuss.posted"));
      await load();
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card discussions-panel" data-testid="discussions-panel">
      <div className="section-heading">
        <h2>{t("discuss.title")}</h2>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>
      <p className="muted">{t("discuss.lead")}</p>
      {notice ? <StatusNotice>{notice}</StatusNotice> : null}
      {error ? <ErrorNotice error={error} /> : null}
      {loading && items.length === 0 ? <p className="muted">{t("common.loading")}</p> : null}
      <ol className="discussion-list">
        {items.length === 0 ? <li className="muted">{t("discuss.empty")}</li> : null}
        {items.map((item) => (
          <li key={item.id}>
            <div className="discussion-head">
              <strong>{item.author_name}</strong>
              <small>{timeFormatter.format(new Date(item.created_at))}</small>
            </div>
            <p>{item.body}</p>
          </li>
        ))}
      </ol>
      {nextCursor ? (
        <button className="button button-ghost button-small" type="button" onClick={() => void load(nextCursor)}>
          {t("common.loadMore")}
        </button>
      ) : null}
      <form className="discussion-form" onSubmit={submit}>
        <textarea
          rows={3}
          maxLength={4000}
          placeholder={t("discuss.placeholder")}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
        />
        <button className="button button-small" type="submit" disabled={busy || !draft.trim()}>
          {busy ? t("discuss.posting") : t("discuss.post")}
        </button>
      </form>
    </section>
  );
}
