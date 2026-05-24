import os

from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter(prefix="/v1", tags=["validation"])


class ValidateRepoPathRequest(BaseModel):
    repo_path: str


class ValidateRepoPathResponse(BaseModel):
    valid: bool
    errors: list[str]


@router.post("/validate-repo-path", response_model=ValidateRepoPathResponse)
async def validate_repo_path(req: ValidateRepoPathRequest) -> ValidateRepoPathResponse:
    errors: list[str] = []

    if not os.path.exists(req.repo_path):
        errors.append(f"路径不存在: {req.repo_path}")
    elif not os.path.isdir(os.path.join(req.repo_path, ".git")):
        errors.append(f"路径不是 git 仓库: {req.repo_path}")

    return ValidateRepoPathResponse(valid=len(errors) == 0, errors=errors)
