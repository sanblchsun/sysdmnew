from starlette.middleware.base import BaseHTTPMiddleware
from fastapi import Request
from fastapi.responses import RedirectResponse

class AuthHTMLMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request: Request, call_next):
        path = request.url.path

        # не трогаем login, static, api
        if (
            path.startswith("/api")
            or path.startswith("/static")
            or path in ("/login", "/logout")
        ):
            return await call_next(request)

        # интересуют только HTML-страницы
        if request.headers.get("accept", "").find("text/html") == -1:
            return await call_next(request)

        try:
            from app.core.authx import auth
            token = await auth.get_access_token_from_request(request)
            auth.verify_token(token, verify_csrf=False)
        except Exception:
            return RedirectResponse("/login", status_code=302)

        return await call_next(request)
