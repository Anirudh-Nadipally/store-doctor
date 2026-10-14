"""Re-score the saved v1.0 sample under the v1.1 rule set (no new requests)."""
import csv, json
W = {"SD-01": 14, "SD-02": 12, "SD-03": 12, "SD-04": 12, "SD-06": 18, "SD-07": 14, "SD-08": 9, "SD-09": 9}
rows = list(csv.DictReader(open("eval/sample-v1.0.csv")))
auc = lambda p, n: sum(1 if a > b else .5 if a == b else 0 for a in p for b in n) / (len(p) * len(n))
score = lambda r: sum(v for k, v in W.items() if r[k] == "1")
P = [score(r) for r in rows if r["group"] == "sellers"]; N = [score(r) for r in rows if r["group"] == "zero"]
Ps = [score(r) for r in rows if r["group"] == "sellers" and int(r["items"])]; Ns = [score(r) for r in rows if r["group"] == "zero" and int(r["items"])]
print(json.dumps({"rules_version": "1.1.0", "auc_all": round(auc(P, N), 3), "auc_stores_with_items": round(auc(Ps, Ns), 3),
                  "mean_score_sellers": round(sum(P) / len(P), 1), "mean_score_zero": round(sum(N) / len(N), 1)}, indent=2))
