from authx import AuthX, AuthXConfig
from app.config import settings


# Конфигурация AuthX
auth_config = AuthXConfig(
    JWT_ALGORITHM="HS256",
    JWT_SECRET_KEY=settings.SECRET_KEY,  # In production, use a secure key and store it in environment variables
    # Configure token locations
    JWT_TOKEN_LOCATION=["headers", "cookies", "json", "query"],
    # Header settings
    JWT_HEADER_TYPE="Bearer",
    # Cookie settings
    JWT_ACCESS_COOKIE_NAME="access_token_cookie",
    JWT_REFRESH_COOKIE_NAME="refresh_token_cookie",
    JWT_COOKIE_SECURE=False,  # Set to True in production with HTTPS
    JWT_COOKIE_CSRF_PROTECT=False,  # Disable CSRF protection for testing
    JWT_ACCESS_CSRF_COOKIE_NAME="csrf_access_token",
    JWT_REFRESH_CSRF_COOKIE_NAME="csrf_refresh_token",
    JWT_ACCESS_CSRF_HEADER_NAME="X-CSRF-TOKEN-Access",
    JWT_REFRESH_CSRF_HEADER_NAME="X-CSRF-TOKEN-Refresh",
    # JSON body settings
    JWT_JSON_KEY="access_token",
    JWT_REFRESH_JSON_KEY="refresh_token",
    # Query string settings
    JWT_QUERY_STRING_NAME="token",
)


auth = AuthX(config=auth_config)
