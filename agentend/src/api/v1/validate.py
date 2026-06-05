import os

from fastapi import APIRouter
from pydantic import BaseModel

from src.workspace.git_ops import GitOps

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


class InitGitRepoRequest(BaseModel):
    repo_path: str


class InitGitRepoResponse(BaseModel):
    success: bool
    errors: list[str]


@router.post("/init-git-repo", response_model=InitGitRepoResponse)
async def init_git_repo(req: InitGitRepoRequest) -> InitGitRepoResponse:
    errors: list[str] = []

    if not os.path.exists(req.repo_path):
        errors.append(f"路径不存在: {req.repo_path}")
    elif not os.path.isdir(req.repo_path):
        errors.append(f"路径不是目录: {req.repo_path}")
    elif os.path.isdir(os.path.join(req.repo_path, ".git")):
        errors.append(f"路径已经是 git 仓库: {req.repo_path}")

    if errors:
        return InitGitRepoResponse(success=False, errors=errors)

    git_ops = GitOps()
    ok = await git_ops.init_repo(req.repo_path)
    if not ok:
        errors.append(f"Git 初始化失败: {req.repo_path}")
        return InitGitRepoResponse(success=False, errors=errors)

    return InitGitRepoResponse(success=True, errors=[])
