use axum::routing::get;
use axum::{Json, Router};
use serde_json::json;
use tokio::net::TcpListener;

#[tokio::main]
async fn main() {
    let app = Router::new().route("/", get(hello));

    let listener = TcpListener::bind("0.0.0.0:8080")
        .await
        .unwrap();
    axum::serve(listener, app).await.unwrap();
}

async fn hello() -> Json<serde_json::Value> {
    Json(json!({
        "message": "hello",
        "status": "ok"
    }))
}