"use client";

import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { api, getAccessToken, getUserId } from "@/lib/api";

// Live shopping — master plan §20. Sellers pin products into a live room;
// buyers check out for real (ledger-settled, inventory-locked).

type Product = {
  id: string;
  title: string;
  description: string;
  price_usd: string;
  inventory: number;
  discount_pct: string;
  image_url: string;
  pinned: boolean;
  seller_id: string;
};

export default function LiveShopPage() {
  const params = useSearchParams();
  const [roomID, setRoomID] = useState(params.get("room") ?? "");
  const [authed, setAuthed] = useState(false);
  const [me, setMe] = useState<string | null>(null);
  const [products, setProducts] = useState<Product[]>([]);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [newProduct, setNewProduct] = useState({
    title: "",
    price_usd: "",
    inventory: "0",
    discount_pct: "0",
    image_url: "",
    description: "",
  });
  const [coupon, setCoupon] = useState("");

  const load = useCallback(async () => {
    if (!roomID) return;
    setError("");
    try {
      const r = await api<{ products: Product[] }>(`/api/live-rooms/${roomID}/products`);
      setProducts(r.products ?? []);
    } catch (e) {
      setError(String(e));
    }
  }, [roomID]);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    setMe(getUserId());
  }, []);

  useEffect(() => {
    if (authed) void load();
  }, [authed, load]);

  const addProduct = async () => {
    try {
      await api(`/api/live-rooms/${roomID}/products`, {
        method: "POST",
        body: JSON.stringify({
          ...newProduct,
          inventory: Number(newProduct.inventory) || 0,
        }),
      });
      setNewProduct({
        title: "", price_usd: "", inventory: "0", discount_pct: "0",
        image_url: "", description: "",
      });
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  const pin = async (p: Product) => {
    try {
      await api(`/api/live-rooms/${roomID}/pin`, {
        method: "POST",
        body: JSON.stringify({ product_id: p.id }),
      });
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  const buy = async (p: Product) => {
    setNote("");
    try {
      const r = await api<{ total_usd: string; order_id: string }>(
        `/api/live-rooms/${roomID}/checkout`,
        {
          method: "POST",
          body: JSON.stringify({ product_id: p.id, quantity: 1, coupon }),
        }
      );
      setNote(`Order ${r.order_id} paid — $${r.total_usd}.`);
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>Live shopping</h1>
        <p>Sign in to buy or sell in a live room.</p>
        <a className="btn" href="/login">Sign in</a>
      </main>
    );
  }

  return (
    <main className="container">
      <h1>Live shopping</h1>
      {error && <p className="error">{error}</p>}
      {note && <p className="note">{note}</p>}

      <section className="card">
        <label>Live room ID</label>
        <input value={roomID} onChange={(e) => setRoomID(e.target.value)} placeholder="room UUID" />
        <button className="btn" onClick={load} disabled={!roomID}>
          Load products
        </button>
      </section>

      <section className="card">
        <h2>Pinned / available products ({products.length})</h2>
        <ul className="list">
          {products.map((p) => (
            <li key={p.id} className="card">
              {p.pinned && <span className="badge">PINNED</span>}
              <strong>{p.title}</strong>
              <div className="muted">{p.description}</div>
              <div>
                ${p.price_usd}
                {Number(p.discount_pct) > 0 && (
                  <span className="muted"> · {p.discount_pct}% off</span>
                )}{" "}
                · {p.inventory} in stock
              </div>
              <div className="row">
                {p.seller_id === me ? (
                  <button className="btn small" onClick={() => pin(p)}>
                    Pin to room
                  </button>
                ) : (
                  <button className="btn small" onClick={() => buy(p)} disabled={p.inventory < 1}>
                    {p.inventory < 1 ? "Sold out" : "Buy 1"}
                  </button>
                )}
              </div>
            </li>
          ))}
          {products.length === 0 && <li className="muted">No products in this room yet.</li>}
        </ul>
        <input
          placeholder="Coupon code (optional)"
          value={coupon}
          onChange={(e) => setCoupon(e.target.value)}
        />
      </section>

      <section className="card">
        <h2>List a product (seller)</h2>
        <input
          placeholder="Title"
          value={newProduct.title}
          onChange={(e) => setNewProduct({ ...newProduct, title: e.target.value })}
        />
        <input
          placeholder="Price USD"
          value={newProduct.price_usd}
          onChange={(e) => setNewProduct({ ...newProduct, price_usd: e.target.value })}
        />
        <input
          placeholder="Inventory"
          value={newProduct.inventory}
          onChange={(e) => setNewProduct({ ...newProduct, inventory: e.target.value })}
        />
        <input
          placeholder="Discount %"
          value={newProduct.discount_pct}
          onChange={(e) => setNewProduct({ ...newProduct, discount_pct: e.target.value })}
        />
        <input
          placeholder="Image URL"
          value={newProduct.image_url}
          onChange={(e) => setNewProduct({ ...newProduct, image_url: e.target.value })}
        />
        <textarea
          placeholder="Description"
          value={newProduct.description}
          onChange={(e) => setNewProduct({ ...newProduct, description: e.target.value })}
        />
        <button className="btn" onClick={addProduct} disabled={!roomID || !newProduct.title}>
          Add product
        </button>
      </section>
    </main>
  );
}
